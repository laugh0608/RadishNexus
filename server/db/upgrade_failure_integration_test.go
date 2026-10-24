//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/laugh0608/RadishNexus/server/internal/testsupport/pgfault"
)

// These tests use the unchanged released migration chain. Faults are installed
// only in fresh disposable databases, after the real historical prefix exists.
func upgradeFailureDatabase(t *testing.T, prefix int) (*pgx.Conn, *pgx.ConnConfig) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("nexus_upgrade_failure_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		admin.Close(ctx)
	})
	config := admin.Config().Copy()
	config.Database = name
	conn := upgradeConnection(t, config)
	upgradeSQL(t, conn, `CREATE TABLE public.radishnexus_schema_migrations(sequence integer PRIMARY KEY,name text NOT NULL,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT clock_timestamp())`)
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations[:prefix] {
		if err := applyMigration(ctx, conn, m); err != nil {
			t.Fatal(err)
		}
	}
	return conn, config
}

func upgradeConnection(t *testing.T, config *pgx.ConnConfig) *pgx.Conn {
	t.Helper()
	conn, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return conn
}

func upgradeSQL(t *testing.T, conn *pgx.Conn, sql string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func upgradeHistory(t *testing.T, conn *pgx.Conn) string {
	return upgradeHistoryThrough(t, conn, 2147483647)
}
func upgradeHistoryThrough(t *testing.T, conn *pgx.Conn, limit int) string {
	t.Helper()
	var result string
	if err := conn.QueryRow(context.Background(), `SELECT coalesce(jsonb_agg(to_jsonb(h) ORDER BY sequence),'[]')::text FROM public.radishnexus_schema_migrations h WHERE sequence<=$1`, limit).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func requireUpgradePrefix(t *testing.T, conn *pgx.Conn, prefix int) {
	t.Helper()
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	checker := &ReadinessChecker{database: conn, expected: migrations[:prefix]}
	if err := checker.CheckReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	var revisionExists bool
	if err := conn.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='radishnexus' AND table_name='workspace_memberships' AND column_name='role_revision')`).Scan(&revisionExists); err != nil {
		t.Fatal(err)
	}
	if revisionExists != (prefix >= 18) {
		t.Fatal("018 DDL and history disagree")
	}
	var unlocked bool
	if err := conn.QueryRow(context.Background(), `SELECT pg_try_advisory_lock($1)`, migrationLockID).Scan(&unlocked); err != nil || !unlocked {
		t.Fatal("migration lock not released", err)
	}
	upgradeSQL(t, conn, `SELECT pg_advisory_unlock($1)`, migrationLockID)
}

func installUpgradeFault(t *testing.T, conn *pgx.Conn, statement string) {
	t.Helper()
	// At INSERT history 018, all its DDL has run but COMMIT has not happened.
	upgradeSQL(t, conn, `CREATE FUNCTION public.upgrade_test_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.sequence=18 THEN `+statement+`; END IF; RETURN NEW; END $$;
CREATE TRIGGER upgrade_test_fault BEFORE INSERT ON public.radishnexus_schema_migrations FOR EACH ROW EXECUTE FUNCTION public.upgrade_test_fault()`)
}

func removeUpgradeFault(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	upgradeSQL(t, conn, `DROP TRIGGER upgrade_test_fault ON public.radishnexus_schema_migrations; DROP FUNCTION public.upgrade_test_fault()`)
}

func waitUpgradeLock(t *testing.T, conn *pgx.Conn, pid uint32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := conn.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted)`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fault did not reach the expected database lock wait")
}

func awaitUpgradeResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("migration did not settle after observed fault")
		return nil
	}
}

func TestUpgradeFailureSQLAndCommittedPrefix(t *testing.T) {
	for _, prefix := range []int{16, 17} {
		t.Run(fmt.Sprintf("from_%03d", prefix), func(t *testing.T) {
			conn, config := upgradeFailureDatabase(t, prefix)
			before := upgradeHistory(t, conn)
			installUpgradeFault(t, conn, `RAISE EXCEPTION 'isolated migration fault' USING ERRCODE='P0001'`)
			err := Migrate(context.Background(), conn)
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "P0001" || !strings.Contains(err.Error(), "record migration 018") {
				t.Fatal("018 precommit fault missing", err)
			}
			observer := upgradeConnection(t, config)
			requireUpgradePrefix(t, observer, 17)
			if prefix == 17 && before != upgradeHistory(t, observer) {
				t.Fatal("failed 018 rewrote history")
			}
			if prefix == 16 && before == upgradeHistory(t, observer) {
				t.Fatal("committed 017 prefix was lost")
			}
			removeUpgradeFault(t, observer)
			if err := Migrate(context.Background(), observer); err != nil {
				t.Fatal(err)
			}
			requireUpgradePrefix(t, observer, 20)
			after := upgradeHistory(t, observer)
			if err := Migrate(context.Background(), observer); err != nil {
				t.Fatal(err)
			}
			if after != upgradeHistory(t, observer) {
				t.Fatal("repeat changed committed history")
			}
		})
	}
}

func TestUpgradeFailureCancellationAndTermination(t *testing.T) {
	for _, mode := range []string{"context_cancel", "terminate_backend"} {
		t.Run(mode, func(t *testing.T) {
			observer, config := upgradeFailureDatabase(t, 17)
			before := upgradeHistory(t, observer)
			const barrier = int64(739140967704145)
			upgradeSQL(t, observer, `SELECT pg_advisory_lock($1)`, barrier)
			defer upgradeSQL(t, observer, `SELECT pg_advisory_unlock($1)`, barrier)
			installUpgradeFault(t, observer, fmt.Sprintf("PERFORM pg_advisory_xact_lock(%d)", barrier))
			runner := upgradeConnection(t, config)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- Migrate(ctx, runner) }()
			waitUpgradeLock(t, observer, runner.PgConn().PID())
			if mode == "context_cancel" {
				cancel()
			} else {
				upgradeSQL(t, observer, `SELECT pg_terminate_backend($1)`, runner.PgConn().PID())
			}
			if err := awaitUpgradeResult(t, done); err == nil {
				t.Fatal("fault reported success")
			}
			runner.Close(context.Background())
			fresh := upgradeConnection(t, config)
			requireUpgradePrefix(t, fresh, 17)
			if before != upgradeHistory(t, fresh) {
				t.Fatal("uncommitted DDL/history survived")
			}
			removeUpgradeFault(t, fresh)
			if err := Migrate(context.Background(), fresh); err != nil {
				t.Fatal(err)
			}
			requireUpgradePrefix(t, fresh, 20)
		})
	}
}

func TestUpgradeFailureLostCommitAcknowledgement(t *testing.T) {
	observer, config := upgradeFailureDatabase(t, 17)
	faultConfig, finish := pgfault.LoseCommit(t, config)
	runner := upgradeConnection(t, faultConfig)
	err := Migrate(context.Background(), runner)
	runner.Close(context.Background())
	finish()
	if err == nil || !strings.Contains(err.Error(), "commit migration 018") {
		t.Fatal("commit outcome was not uncertain", err)
	}
	fresh := upgradeConnection(t, config)
	requireUpgradePrefix(t, fresh, 18)
	before := upgradeHistory(t, observer)
	if err := Migrate(context.Background(), fresh); err != nil {
		t.Fatal(err)
	}
	if before != upgradeHistoryThrough(t, fresh, 18) {
		t.Fatal("uncertain commit replay changed its committed prefix")
	}
}

func TestUpgradeFailureConcurrentRunnersAndTimeout(t *testing.T) {
	observer, config := upgradeFailureDatabase(t, 17)
	upgradeSQL(t, observer, `SELECT pg_advisory_lock($1)`, migrationLockID)
	defer upgradeSQL(t, observer, `SELECT pg_advisory_unlock($1)`, migrationLockID)
	deadline, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	waiter := upgradeConnection(t, config)
	start := time.Now()
	if err := Migrate(deadline, waiter); err == nil {
		t.Fatal("locked runner did not time out")
	}
	waiter.Close(context.Background())
	t.Logf("short test request budget 150ms, observed settlement %s; not a CLI hard-limit claim", time.Since(start))
	a, b := upgradeConnection(t, config), upgradeConnection(t, config)
	da, db := make(chan error, 1), make(chan error, 1)
	go func() { da <- Migrate(context.Background(), a) }()
	go func() { db <- Migrate(context.Background(), b) }()
	waitUpgradeLock(t, observer, a.PgConn().PID())
	waitUpgradeLock(t, observer, b.PgConn().PID())
	upgradeSQL(t, observer, `SELECT pg_advisory_unlock($1)`, migrationLockID)
	for _, done := range []<-chan error{da, db} {
		if err := awaitUpgradeResult(t, done); err != nil {
			t.Fatal(err)
		}
	}
	requireUpgradePrefix(t, observer, 20)
}

func TestUpgradeFailureHistoryDriftAndUntrackedDDL(t *testing.T) {
	for _, change := range []struct{ name, sql string }{
		{"gap", `DELETE FROM public.radishnexus_schema_migrations WHERE sequence=2`},
		{"checksum", `UPDATE public.radishnexus_schema_migrations SET checksum=repeat('0',64) WHERE sequence=1`},
	} {
		t.Run(change.name, func(t *testing.T) {
			conn, _ := upgradeFailureDatabase(t, 17)
			upgradeSQL(t, conn, change.sql)
			before := upgradeHistory(t, conn)
			if err := Migrate(context.Background(), conn); err == nil {
				t.Fatal("migration accepted history drift")
			}
			if upgradeHistory(t, conn) != before {
				t.Fatal("migration rewrote invalid history")
			}
		})
	}
	t.Run("manual_DDL_is_outside_readiness", func(t *testing.T) {
		conn, _ := upgradeFailureDatabase(t, 20)
		upgradeSQL(t, conn, `ALTER TABLE radishnexus.workspaces ADD COLUMN manual_upgrade_test text`)
		probe, err := NewReadinessChecker(conn)
		if err != nil {
			t.Fatal(err)
		}
		if err := probe.CheckReady(context.Background()); err != nil {
			t.Fatal("readiness contract unexpectedly changed", err)
		}
		// Operational preflight must still reject a known unreviewed alteration.
		// A successful exact-history probe does not certify arbitrary DDL.
	})
}
