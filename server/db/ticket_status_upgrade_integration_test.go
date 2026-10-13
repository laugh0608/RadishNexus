//go:build integration

package db

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTicketStatusUpgradeFrom015(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL required")
	}
	ctx := context.Background()
	admin, e := pgx.Connect(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close(ctx)
	for _, status := range []string{"open", "in-progress", "done", "canceled"} {
		t.Run(status, func(t *testing.T) {
			name := fmt.Sprintf("nexus_status_upgrade_%d", time.Now().UnixNano())
			if _, e := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); e != nil {
				t.Fatal(e)
			}
			defer func() {
				if _, e := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); e != nil {
					t.Error(e)
				}
			}()
			config := admin.Config().Copy()
			config.Database = name
			conn, e := pgx.ConnectConfig(ctx, config)
			if e != nil {
				t.Fatal(e)
			}
			defer conn.Close(ctx)
			if _, e = conn.Exec(ctx, `CREATE TABLE public.radishnexus_schema_migrations(sequence integer PRIMARY KEY,name text NOT NULL,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT clock_timestamp())`); e != nil {
				t.Fatal(e)
			}
			migrations, e := loadMigrations(migrationFiles)
			if e != nil {
				t.Fatal(e)
			}
			for _, m := range migrations[:15] {
				if e = applyMigration(ctx, conn, m); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = conn.Exec(ctx, `
INSERT INTO radishnexus.users(id,display_name) VALUES('usr_old','Old');
INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_old','Old');
INSERT INTO radishnexus.teams(id,workspace_id,name) VALUES('tem_old','wrk_old','Old');
INSERT INTO radishnexus.projects(id,workspace_id,key,name,owner_team_id,visibility,status,created_by_kind,created_by_id) VALUES('prj_old','wrk_old','OLD','Old','tem_old','workspace','active','user','usr_old');
INSERT INTO radishnexus.tickets(id,workspace_id,governing_project_id,title,created_by) VALUES('tkt_old','wrk_old','prj_old','Old work','usr_old');`); e != nil {
				t.Fatal(e)
			}
			if _, e = conn.Exec(ctx, `UPDATE radishnexus.tickets SET status=$1`, status); e != nil {
				t.Fatal(e)
			}
			var before string
			if e = conn.QueryRow(ctx, `SELECT to_jsonb(t)::text FROM radishnexus.tickets t`).Scan(&before); e != nil {
				t.Fatal(e)
			}
			old := &ReadinessChecker{database: conn, expected: migrations[:15]}
			fresh, e := NewReadinessChecker(conn)
			if e != nil {
				t.Fatal(e)
			}
			if e = old.CheckReady(ctx); e != nil {
				t.Fatal(e)
			}
			if e = fresh.CheckReady(ctx); e == nil {
				t.Fatal("new binary accepted 015")
			}
			e = Migrate(ctx, conn)
			if status != "open" {
				if e == nil || !strings.Contains(e.Error(), "open-only baseline") {
					t.Fatal("anomaly upgraded", e)
				}
				if e = old.CheckReady(ctx); e != nil {
					t.Fatal("failed migration changed history", e)
				}
				var after string
				if e = conn.QueryRow(ctx, `SELECT to_jsonb(t)::text FROM radishnexus.tickets t`).Scan(&after); e != nil || before != after {
					t.Fatal("failed migration changed Ticket", e)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = old.CheckReady(ctx); e == nil {
				t.Fatal("old binary accepted 016")
			}
			if e = fresh.CheckReady(ctx); e != nil {
				t.Fatal(e)
			}
			var after string
			var revision, events int
			if e = conn.QueryRow(ctx, `SELECT (to_jsonb(t)-'status_revision')::text,status_revision FROM radishnexus.tickets t`).Scan(&after, &revision); e != nil || before != after || revision != 1 {
				t.Fatal("migration rewrote old facts", e)
			}
			if e = conn.QueryRow(ctx, `SELECT count(*) FROM radishnexus.domain_events`).Scan(&events); e != nil || events != 0 {
				t.Fatal("invented history", e)
			}
		})
	}
}
