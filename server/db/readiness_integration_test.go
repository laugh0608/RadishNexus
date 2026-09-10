//go:build integration

package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReadinessRequiresExactHistoryWithoutWriting(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	name := fmt.Sprintf("nexus_readiness_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	}()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	checker, err := NewReadinessChecker(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := checker.CheckReady(ctx); err == nil {
		t.Fatal("empty database reported ready")
	}
	var historyExists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.radishnexus_schema_migrations') IS NOT NULL").Scan(&historyExists); err != nil || historyExists {
		t.Fatal("readiness created migration history", err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, connection.Conn()); err != nil {
		connection.Release()
		t.Fatal(err)
	}
	connection.Release()
	if err := checker.CheckReady(ctx); err != nil {
		t.Fatal("fully migrated database not ready", err)
	}
	// A read-only PostgreSQL transaction enforces the probe's no-write contract.
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	readonly, err := NewReadinessChecker(tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := readonly.CheckReady(ctx); err != nil {
		t.Fatal("read-only probe failed", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct{ name, change string }{
		{"empty history", "DELETE FROM public.radishnexus_schema_migrations"},
		{"pending migration", "DELETE FROM public.radishnexus_schema_migrations WHERE sequence = (SELECT max(sequence) FROM public.radishnexus_schema_migrations)"},
		{"gap", "DELETE FROM public.radishnexus_schema_migrations WHERE sequence = 2"},
		{"name drift", "UPDATE public.radishnexus_schema_migrations SET name = 'changed' WHERE sequence = 1"},
		{"checksum drift", "UPDATE public.radishnexus_schema_migrations SET checksum = repeat('0',64) WHERE sequence = 1"},
		{"future migration", "INSERT INTO public.radishnexus_schema_migrations(sequence,name,checksum) SELECT max(sequence)+1,'future',repeat('0',64) FROM public.radishnexus_schema_migrations"},
		{"missing history", "DROP TABLE public.radishnexus_schema_migrations"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err := tx.Exec(ctx, test.change); err != nil {
				t.Fatal(err)
			}
			probe, err := NewReadinessChecker(tx)
			if err != nil {
				t.Fatal(err)
			}
			if err := probe.CheckReady(ctx); err == nil {
				t.Fatal("incompatible schema reported ready")
			}
		})
	}
	oldBinary := &ReadinessChecker{database: pool, expected: checker.expected[:len(checker.expected)-1]}
	if err := oldBinary.CheckReady(ctx); err == nil {
		t.Fatal("older binary accepted newer database")
	}
	// A locked history must honor the HTTP caller's deadline, then recover.
	lock, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(ctx)
	if _, err := lock.Exec(ctx, "LOCK TABLE public.radishnexus_schema_migrations IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err := checker.CheckReady(deadline); err == nil || deadline.Err() == nil {
		t.Fatal("blocked history did not honor deadline", err)
	}
	if err := lock.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := checker.CheckReady(ctx); err != nil {
		t.Fatal("probe did not recover after database repaired", err)
	}
	pool.Close()
	if err := checker.CheckReady(ctx); err == nil {
		t.Fatal("closed pool reported ready")
	}
}
