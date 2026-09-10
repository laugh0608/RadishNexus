package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type migrationQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// ReadinessChecker checks an exact migration history match without DDL or
// writes. It shares the migration runner's artifact and history validation.
// A successful check is a point-in-time observation, not a deployment lock or
// an audit of manual schema edits outside the migration system.
type ReadinessChecker struct {
	database migrationQuerier
	expected []migration
}

func NewReadinessChecker(database migrationQuerier) (*ReadinessChecker, error) {
	expected, err := loadMigrations(migrationFiles)
	if err != nil {
		return nil, err
	}
	return &ReadinessChecker{database: database, expected: expected}, nil
}

func (checker *ReadinessChecker) CheckReady(ctx context.Context) error {
	applied, err := loadApplied(ctx, checker.database)
	if err != nil {
		return err
	}
	if err := validateAppliedHistory(applied, checker.expected); err != nil {
		return err
	}
	if len(applied) != len(checker.expected) {
		return fmt.Errorf("database has %d of %d required migrations", len(applied), len(checker.expected))
	}
	return nil
}

func validateAppliedHistory(applied, expected []migration) error {
	if len(applied) > len(expected) {
		return fmt.Errorf("database migration %d is newer than embedded migration %d", len(applied), len(expected))
	}
	for index, recorded := range applied {
		artifact := expected[index]
		if recorded.sequence != artifact.sequence || recorded.name != artifact.name || recorded.checksum != artifact.checksum {
			return fmt.Errorf("migration history drift at sequence %d", index+1)
		}
	}
	return nil
}
