//go:build integration

package postgres_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"os"
	"testing"
	"time"
)

func TestFirstVisitSetupAtomicWithCLI(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	database := fmt.Sprintf("nexus_setup_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{database}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{database}.Sanitize()); err != nil {
			t.Error(err)
		}
	}()
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = database
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Migrate(ctx, conn.Conn())
	conn.Release()
	if err != nil {
		t.Fatal(err)
	}
	store := authpostgres.New(pool)
	auth := authn.NewService(store, authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	code := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	setup, err := authn.NewSetupService(auth, store, code)
	if err != nil {
		t.Fatal(err)
	}
	input := authn.BootstrapInput{Email: "first@example.test", DisplayName: "First Owner", WorkspaceName: "First Workspace", Password: "synthetic first setup password"}
	if state, err := setup.Status(ctx); err != nil || state != "required" {
		t.Fatal(state, err)
	}
	if err := setup.Complete(ctx, "wrong", input); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal(err)
	}
	// Failure after account/workspace insertion must not leave partial bootstrap.
	if _, err = pool.Exec(ctx, `ALTER TABLE radishnexus.local_credentials ADD CONSTRAINT setup_test_failure CHECK(email <> 'first@example.test')`); err != nil {
		t.Fatal(err)
	}
	if err = setup.Complete(ctx, code, input); err == nil {
		t.Fatal("injected failure swallowed")
	}
	if state, err := setup.Status(ctx); err != nil || state != "required" {
		t.Fatal("partial account", state, err)
	}
	var partial int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM radishnexus.users)+(SELECT count(*) FROM radishnexus.workspaces)`).Scan(&partial); err != nil || partial != 0 {
		t.Fatal("partial rows", partial, err)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE radishnexus.local_credentials DROP CONSTRAINT setup_test_failure`); err != nil {
		t.Fatal(err)
	}
	// Separate services represent two Web processes plus the CLI bootstrap path.
	second, _ := authn.NewSetupService(auth, store, code)
	start := make(chan struct{})
	out := make(chan error, 3)
	for _, s := range []*authn.SetupService{setup, second} {
		go func() { <-start; out <- s.Complete(ctx, code, input) }()
	}
	go func() { <-start; _, err := auth.Bootstrap(ctx, input); out <- err }()
	close(start)
	successes, conflicts := 0, 0
	for range 3 {
		err := <-out
		if err == nil {
			successes++
		} else if errors.Is(err, authn.ErrAlreadyBootstrapped) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 2 {
		t.Fatal(successes, conflicts)
	}
	session, err := auth.Login(ctx, authn.LoginInput{Email: input.Email, Password: input.Password})
	if err != nil || len(session.Account.Workspaces) != 1 || session.Account.Workspaces[0].Role != "owner" {
		t.Fatal("first owner login", err)
	}
	// Disabled accounts still permanently close initialization, including on restart.
	if _, err = pool.Exec(ctx, `UPDATE radishnexus.user_accounts SET status='disabled'`); err != nil {
		t.Fatal(err)
	}
	restarted, _ := authn.NewSetupService(auth, store, "")
	if state, err := restarted.Status(ctx); err != nil || state != "complete" {
		t.Fatal(state, err)
	}
	if err := setup.Complete(ctx, code, input); !errors.Is(err, authn.ErrAlreadyBootstrapped) {
		t.Fatal(err)
	}
}
