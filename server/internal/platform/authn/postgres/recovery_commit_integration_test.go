//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/testsupport/pgfault"
)

// A wire proxy discards the actual PostgreSQL COMMIT acknowledgement after the
// server has committed. No mock transaction or synthetic commit error is used.
func TestRecoveryLostCommitAcknowledgement(t *testing.T) {
	f := newRecoveryFixture(t)
	pool, finish := lostCommitPool(t, f.ctx, f.pool)
	service := authn.NewRecoveryService(authpostgres.New(pool), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{})
	input := f.input(t, 1)
	_, err := service.Apply(f.ctx, input)
	finish()
	if !errors.Is(err, authn.ErrRecoveryUnconfirmed) {
		t.Fatal("commit loss was not uncertain", err)
	}
	status, err := f.service.Status(f.ctx, input.UserID, input.OperationID)
	if err != nil || status.Result != "applied" {
		t.Fatal("committed operation not found", err)
	}
	if _, err = f.service.Apply(f.ctx, input); !errors.Is(err, authn.ErrRecoveryRecorded) {
		t.Fatal("uncertain operation replayed", err)
	}
	if _, err = f.auth.Login(f.ctx, authn.LoginInput{Email: recoveryEmail, Password: recoveryNewPassword}); err != nil {
		t.Fatal("committed password absent", err)
	}
}

// Both recovery and handoff exercise the same real PostgreSQL protocol fault.
func lostCommitPool(t *testing.T, ctx context.Context, source *pgxpool.Pool) (*pgxpool.Pool, func()) {
	t.Helper()
	config := source.Config().Copy()
	connConfig, finish := pgfault.LoseCommit(t, config.ConnConfig)
	config.ConnConfig = connConfig
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	return pool, func() { pool.Close(); finish() }
}
