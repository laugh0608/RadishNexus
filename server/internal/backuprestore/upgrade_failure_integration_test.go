//go:build integration

package backuprestore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/db"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
)

// The real restore CLI runs the real pg_restore. A test-only executable injects
// a PostgreSQL error at a known phase, never a fabricated tool return value.
func TestBackupRestoreUpgradeFailures(t *testing.T) {
	sourceURL, targetURL := os.Getenv("SOURCE_DATABASE_URL"), os.Getenv("TARGET_DATABASE_URL")
	cli := os.Getenv("RADISHNEXUS_RESTORE_TEST_BIN")
	if sourceURL == "" || targetURL == "" || cli == "" {
		t.Skip("disposable source, target and restore CLI required")
	}
	ctx := context.Background()
	sourceURL = recoveryBackupDatabase(t, ctx, sourceURL)
	source := connectIntegrationDatabase(t, ctx, sourceURL)
	defer source.Close(ctx)
	if err := db.Migrate(ctx, source); err != nil {
		t.Fatal(err)
	}
	pool := connectIntegrationPool(t, ctx, sourceURL)
	defer pool.Close()
	seedBackupGoldenPath(t, ctx, pool)
	baseline := snapshotIncludedTables(t, ctx, pool)
	backup := filepath.Join(t.TempDir(), "backup")
	if _, err := NewService().Backup(ctx, source, sourceURL, backup); err != nil {
		t.Fatal(err)
	}
	realRestore, err := exec.LookPath("pg_restore")
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"dump_transaction", "dump_disconnect", "activity_rebuild", "wrong_tool_major"} {
		t.Run(phase, func(t *testing.T) {
			commandContext, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			dsn := recoveryBackupDatabase(t, ctx, targetURL)
			target := connectIntegrationDatabase(t, ctx, dsn)
			defer target.Close(ctx)
			wrapper := filepath.Join(t.TempDir(), "pg_restore_fault")
			// Both scripts act only after the target's empty preflight. Their SQL
			// faults are confined to this newly created test database.
			script := `#!/bin/sh
set -eu
case " $* " in
  *" --dbname= "*)
`
			if phase == "dump_transaction" {
				script += `psql -X -q -v ON_ERROR_STOP=1 <<'SQL'
CREATE FUNCTION public.upgrade_restore_fault() RETURNS event_trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'upgrade_restore_dump_fault'; END $$;
CREATE EVENT TRIGGER upgrade_restore_fault ON ddl_command_end WHEN TAG IN ('CREATE TABLE') EXECUTE FUNCTION public.upgrade_restore_fault();
SQL
`
			}
			if phase == "dump_disconnect" {
				if _, err := target.Exec(ctx, `SELECT pg_advisory_lock(904319)`); err != nil {
					t.Fatal(err)
				}
				defer target.Exec(ctx, `SELECT pg_advisory_unlock(904319)`)
				script += `psql -X -q -v ON_ERROR_STOP=1 <<'SQL'
CREATE FUNCTION public.upgrade_restore_fault() RETURNS event_trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(904319); END $$;
CREATE EVENT TRIGGER upgrade_restore_fault ON ddl_command_end WHEN TAG IN ('CREATE TABLE') EXECUTE FUNCTION public.upgrade_restore_fault();
SQL
`
			}
			script += `;;
esac
`
			if phase == "wrong_tool_major" {
				script += `if [ "$1" = "--version" ]; then echo 'pg_restore (PostgreSQL) 16.0'; exit 0; fi
`
			}
			script += `"$RADISHNEXUS_REAL_PG_RESTORE" "$@"
`
			if phase == "activity_rebuild" {
				script += `case " $* " in
 *" --dbname= "*) psql -X -q -v ON_ERROR_STOP=1 <<'SQL'
CREATE FUNCTION public.upgrade_activity_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'upgrade_restore_activity_fault'; END $$;
CREATE TRIGGER upgrade_activity_fault BEFORE INSERT ON radishnexus.activity_items FOR EACH ROW EXECUTE FUNCTION public.upgrade_activity_fault();
SQL
;;
esac
`
			}
			if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(commandContext, cli, "--input", backup)
			command.Env = append(os.Environ(), "DATABASE_URL="+dsn, "RADISHNEXUS_DATABASE_PASSWORD_FILE=", "RADISHNEXUS_PG_RESTORE="+wrapper, "RADISHNEXUS_REAL_PG_RESTORE="+realRestore)
			type outcome struct {
				output []byte
				err    error
			}
			done := make(chan outcome, 1)
			go func() { output, err := command.CombinedOutput(); done <- outcome{output, err} }()
			if phase == "dump_disconnect" {
				var pid int
				deadline := time.Now().Add(10 * time.Second)
				for time.Now().Before(deadline) {
					if err := target.QueryRow(ctx, `SELECT coalesce(max(a.pid),0) FROM pg_stat_activity a JOIN pg_locks l ON l.pid=a.pid WHERE a.datname=current_database() AND a.application_name='radishnexus-restore' AND l.locktype='advisory' AND l.objid=904319 AND NOT l.granted`).Scan(&pid); err != nil {
						t.Fatal(err)
					}
					if pid != 0 {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if pid == 0 {
					t.Fatal("pg_restore did not reach CREATE TABLE barrier")
				}
				if _, err := target.Exec(ctx, `SELECT pg_terminate_backend($1)`, pid); err != nil {
					t.Fatal(err)
				}
			}
			var result outcome
			select {
			case result = <-done:
			case <-time.After(15 * time.Second):
				t.Fatal("restore CLI did not settle after fault")
			}
			output, err := result.output, result.err
			if err == nil || strings.Contains(string(output), "restore completed:") {
				t.Fatal("faulty CLI reported usable restore")
			}
			want := map[string]string{"dump_transaction": "upgrade_restore_dump_fault", "dump_disconnect": "restore PostgreSQL dump", "activity_rebuild": "upgrade_restore_activity_fault", "wrong_tool_major": "pg_restore major 16"}[phase]
			if !strings.Contains(string(output), want) {
				t.Fatal("fault did not reach expected restore stage: " + phase)
			}
			if phase == "activity_rebuild" {
				restored := connectIntegrationPool(t, ctx, dsn)
				defer restored.Close()
				if !reflect.DeepEqual(baseline, snapshotIncludedTables(t, ctx, restored)) {
					t.Fatal("authoritative restore did not complete before projection fault")
				}
				if snapshotTable(t, ctx, restored, "radishnexus.activity_items") != "[]" {
					t.Fatal("failed rebuild left partial Activity")
				}
				if _, err := NewService().Restore(ctx, target, dsn, backup); err == nil {
					t.Fatal("failed nonempty target accepted overwrite")
				}
			} else {
				assertIntegrationTargetEmpty(t, ctx, target)
			}
			// Even a rolled-back dump is not silently retried in place. Keep its
			// target until fixture cleanup and verify an independent empty target.
			freshURL := recoveryBackupDatabase(t, ctx, targetURL)
			fresh := connectIntegrationDatabase(t, ctx, freshURL)
			defer fresh.Close(ctx)
			if _, err := NewService().Restore(ctx, fresh, freshURL, backup); err != nil {
				t.Fatal(err)
			}
			restored := connectIntegrationPool(t, ctx, freshURL)
			defer restored.Close()
			if _, err := goldenpostgres.New(restored).RebuildActivityProjection(ctx); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(baseline, snapshotIncludedTables(t, ctx, restored)) {
				t.Fatal("fresh retry snapshot mismatch")
			}
			if !reflect.DeepEqual(baseline, snapshotIncludedTables(t, ctx, pool)) {
				t.Fatal("restore modified source")
			}
		})
	}
}
