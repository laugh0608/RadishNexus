// Package db owns the embedded, forward-only PostgreSQL migrations.
package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	migrationLockID = int64(739140967704144)
	downMarker      = "---- create above / drop below ----"
)

var migrationNamePattern = regexp.MustCompile(`^(\d{3})_([a-z0-9_]+)\.sql$`)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	sequence int
	name     string
	checksum string
	upSQL    string
}

// MigrationRecord is the stable, serializable identity of one embedded
// migration artifact. The SQL body is deliberately not exposed.
type MigrationRecord struct {
	Sequence int    `json:"sequence"`
	Name     string `json:"name"`
	Checksum string `json:"checksum"`
}

// CurrentMigrationHistory returns the exact migration artifact identities
// embedded in this binary. Backup manifests use this to reject history drift
// without copying migration SQL into the artifact.
func CurrentMigrationHistory() ([]MigrationRecord, error) {
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		return nil, err
	}
	records := make([]MigrationRecord, len(migrations))
	for index, item := range migrations {
		records[index] = MigrationRecord{
			Sequence: item.sequence,
			Name:     item.name,
			Checksum: item.checksum,
		}
	}
	return records, nil
}

// Migrate applies every pending migration in its own transaction. It refuses
// changed history and concurrent runners. Rollback is deliberately not an
// automated production capability: restore and forward repair are deployment
// concerns that require their own evidence.
func Migrate(ctx context.Context, connection *pgx.Conn) (err error) {
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		return err
	}

	if _, err := connection.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	defer func() {
		_, unlockErr := connection.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLockID)
		if err == nil && unlockErr != nil {
			err = fmt.Errorf("release migration advisory lock: %w", unlockErr)
		}
	}()

	if _, err := connection.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS public.radishnexus_schema_migrations (
			sequence integer PRIMARY KEY CHECK (sequence > 0),
			name text NOT NULL,
			checksum text NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT clock_timestamp()
		)
	`); err != nil {
		return fmt.Errorf("ensure migration history table: %w", err)
	}

	applied, err := loadApplied(ctx, connection)
	if err != nil {
		return err
	}
	if len(applied) > len(migrations) {
		return fmt.Errorf("database migration %d is newer than embedded migration %d", len(applied), len(migrations))
	}
	for index, recorded := range applied {
		expected := migrations[index]
		if recorded.sequence != expected.sequence || recorded.name != expected.name || recorded.checksum != expected.checksum {
			return fmt.Errorf("migration history drift at sequence %d", index+1)
		}
	}

	for _, pending := range migrations[len(applied):] {
		if err := applyMigration(ctx, connection, pending); err != nil {
			return err
		}
	}
	return nil
}

func loadMigrations(root fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(root, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })

	migrations := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := migrationNamePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		sequence, err := strconv.Atoi(matches[1])
		if err != nil {
			return nil, fmt.Errorf("parse migration sequence %q: %w", entry.Name(), err)
		}
		if sequence != len(migrations)+1 {
			return nil, fmt.Errorf("migration sequence must be continuous: got %d after %d", sequence, len(migrations))
		}

		body, err := fs.ReadFile(root, "migrations/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		upSQL := strings.TrimSpace(strings.SplitN(string(body), downMarker, 2)[0])
		if upSQL == "" {
			return nil, fmt.Errorf("migration %q has no forward SQL", entry.Name())
		}
		digest := sha256.Sum256(body)
		migrations = append(migrations, migration{
			sequence: sequence,
			name:     matches[2],
			checksum: hex.EncodeToString(digest[:]),
			upSQL:    upSQL,
		})
	}
	if len(migrations) == 0 {
		return nil, fmt.Errorf("no embedded migrations")
	}
	return migrations, nil
}

func loadApplied(ctx context.Context, connection *pgx.Conn) ([]migration, error) {
	rows, err := connection.Query(ctx, `
		SELECT sequence, name, checksum
		FROM public.radishnexus_schema_migrations
		ORDER BY sequence
	`)
	if err != nil {
		return nil, fmt.Errorf("read migration history: %w", err)
	}
	defer rows.Close()

	var applied []migration
	for rows.Next() {
		var recorded migration
		if err := rows.Scan(&recorded.sequence, &recorded.name, &recorded.checksum); err != nil {
			return nil, fmt.Errorf("scan migration history: %w", err)
		}
		if recorded.sequence != len(applied)+1 {
			return nil, fmt.Errorf("database migration history has a gap before sequence %d", recorded.sequence)
		}
		applied = append(applied, recorded)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate migration history: %w", err)
	}
	return applied, nil
}

func applyMigration(ctx context.Context, connection *pgx.Conn, pending migration) (err error) {
	tx, err := connection.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %03d_%s: %w", pending.sequence, pending.name, err)
	}
	defer func() {
		rollbackErr := tx.Rollback(context.WithoutCancel(ctx))
		if err == nil && rollbackErr != nil && rollbackErr != pgx.ErrTxClosed {
			err = fmt.Errorf("rollback migration %03d_%s: %w", pending.sequence, pending.name, rollbackErr)
		}
	}()

	if _, err := tx.Exec(ctx, pending.upSQL); err != nil {
		return fmt.Errorf("apply migration %03d_%s: %w", pending.sequence, pending.name, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO public.radishnexus_schema_migrations (sequence, name, checksum)
		VALUES ($1, $2, $3)
	`, pending.sequence, pending.name, pending.checksum); err != nil {
		return fmt.Errorf("record migration %03d_%s: %w", pending.sequence, pending.name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %03d_%s: %w", pending.sequence, pending.name, err)
	}
	return nil
}
