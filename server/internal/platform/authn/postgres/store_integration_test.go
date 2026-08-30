//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type integrationSecrets struct {
	mu     sync.Mutex
	ids    []string
	tokens []string
}

func (secrets *integrationSecrets) NewID(string) (string, error) {
	secrets.mu.Lock()
	defer secrets.mu.Unlock()
	value := secrets.ids[0]
	secrets.ids = secrets.ids[1:]
	return value, nil
}

func (secrets *integrationSecrets) NewToken() (string, error) {
	secrets.mu.Lock()
	defer secrets.mu.Unlock()
	value := secrets.tokens[0]
	secrets.tokens = secrets.tokens[1:]
	return value, nil
}

type integrationClock struct{ now time.Time }

func (clock integrationClock) Now() time.Time { return clock.now }

func TestLocalIdentityBootstrapLoginAndSessionLifecycle(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for integration tests")
	}
	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgx.Connect() error = %v", err)
	}
	if err := db.Migrate(ctx, connection); err != nil {
		t.Fatalf("db.Migrate() error = %v", err)
	}
	if err := connection.Close(ctx); err != nil {
		t.Fatalf("migration connection Close() error = %v", err)
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	store := authpostgres.New(pool)
	now := time.Date(2026, 8, 30, 6, 0, 0, 0, time.UTC)
	password := "correct horse battery staple"

	type bootstrapOutcome struct {
		result authn.BootstrapResult
		err    error
	}
	outcomes := make(chan bootstrapOutcome, 2)
	start := make(chan struct{})
	services := []*authn.Service{
		authn.NewService(
			store,
			authn.NewArgon2idHasher(),
			&integrationSecrets{ids: []string{"usr_bootstrap_a", "wrk_bootstrap_a"}},
			integrationClock{now: now},
		),
		authn.NewService(
			store,
			authn.NewArgon2idHasher(),
			&integrationSecrets{ids: []string{"usr_bootstrap_b", "wrk_bootstrap_b"}},
			integrationClock{now: now},
		),
	}
	for _, service := range services {
		go func(service *authn.Service) {
			<-start
			result, err := service.Bootstrap(ctx, authn.BootstrapInput{
				LoginName:     "admin",
				DisplayName:   "First Admin",
				WorkspaceName: "First Workspace",
				Password:      password,
			})
			outcomes <- bootstrapOutcome{result: result, err: err}
		}(service)
	}
	close(start)

	var bootstrap authn.BootstrapResult
	var successes, conflicts int
	for range 2 {
		outcome := <-outcomes
		switch {
		case outcome.err == nil:
			successes++
			bootstrap = outcome.result
		case errors.Is(outcome.err, authn.ErrAlreadyBootstrapped):
			conflicts++
		default:
			t.Fatalf("Bootstrap() unexpected error = %v", outcome.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("bootstrap outcomes: successes = %d, conflicts = %d", successes, conflicts)
	}

	var membershipRole string
	if err := pool.QueryRow(ctx, `
		SELECT role
		FROM radishnexus.workspace_memberships
		WHERE workspace_id = $1 AND user_id = $2
	`, bootstrap.WorkspaceID, bootstrap.UserID).Scan(&membershipRole); err != nil {
		t.Fatalf("read bootstrap membership: %v", err)
	}
	if membershipRole != "owner" {
		t.Fatalf("bootstrap membership role = %q, want owner", membershipRole)
	}

	loginService := authn.NewService(
		store,
		authn.NewArgon2idHasher(),
		&integrationSecrets{
			ids:    []string{"ses_integration"},
			tokens: []string{integrationToken(1), integrationToken(2)},
		},
		integrationClock{now: now},
	)
	for attempt := 0; attempt < authn.LoginFailureLimit; attempt++ {
		_, err := loginService.Login(ctx, authn.LoginInput{LoginName: "admin", Password: "wrong-password"})
		if !errors.Is(err, authn.ErrInvalidCredentials) {
			t.Fatalf("Login() failed attempt %d error = %v", attempt+1, err)
		}
	}
	if _, err := loginService.Login(ctx, authn.LoginInput{LoginName: "admin", Password: password}); !errors.Is(err, authn.ErrInvalidCredentials) {
		t.Fatalf("Login() during lock error = %v", err)
	}

	unlockedAt := now.Add(authn.LoginLockDuration + time.Second)
	sessionService := authn.NewService(
		store,
		authn.NewArgon2idHasher(),
		&integrationSecrets{
			ids:    []string{"ses_integration"},
			tokens: []string{integrationToken(1), integrationToken(2)},
		},
		integrationClock{now: unlockedAt},
	)
	session, err := sessionService.Login(ctx, authn.LoginInput{LoginName: "admin", Password: password})
	if err != nil {
		t.Fatalf("Login() after lock error = %v", err)
	}
	if session.Account.User.ID != bootstrap.UserID || len(session.Account.Workspaces) != 1 ||
		session.Account.Workspaces[0].Role != "owner" {
		t.Fatalf("created session account = %#v", session.Account)
	}

	var storedTokenHex string
	if err := pool.QueryRow(ctx, `
		SELECT encode(token_digest, 'hex')
		FROM radishnexus.user_sessions
		WHERE id = 'ses_integration'
	`).Scan(&storedTokenHex); err != nil {
		t.Fatalf("read stored session digest: %v", err)
	}
	if storedTokenHex == session.Token || storedTokenHex == session.CSRFToken {
		t.Fatal("database contains a raw session secret")
	}
	if _, err := pool.Exec(ctx, `
		DELETE FROM radishnexus.user_sessions WHERE id = 'ses_integration'
	`); err == nil {
		t.Fatal("active user session deletion error = nil")
	}
	var failedCount int
	var lockedUntil *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT failed_login_count, locked_until
		FROM radishnexus.local_accounts
		WHERE user_id = $1
	`, bootstrap.UserID).Scan(&failedCount, &lockedUntil); err != nil {
		t.Fatalf("read reset login state: %v", err)
	}
	if failedCount != 0 || lockedUntil != nil {
		t.Fatalf("login state after success = count %d, locked until %v", failedCount, lockedUntil)
	}

	verified, err := sessionService.ResolveWorkspace(ctx, session.Token, bootstrap.WorkspaceID)
	if err != nil {
		t.Fatalf("ResolveWorkspace() error = %v", err)
	}
	if verified != (authn.VerifiedUser{UserID: bootstrap.UserID, WorkspaceID: bootstrap.WorkspaceID}) {
		t.Fatalf("ResolveWorkspace() = %#v", verified)
	}
	if _, err := sessionService.ResolveWorkspace(ctx, session.Token, "wrk_not_a_member"); !errors.Is(err, authz.ErrForbidden) {
		t.Fatalf("ResolveWorkspace() cross-workspace error = %v, want forbidden", err)
	}

	if err := sessionService.VerifyCSRF(ctx, session.Token, integrationToken(9)); !errors.Is(err, authn.ErrInvalidCSRFToken) {
		t.Fatalf("VerifyCSRF() wrong token error = %v", err)
	}
	if err := sessionService.RevokeSession(ctx, session.Token, session.CSRFToken); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if _, err := sessionService.ResolveSession(ctx, session.Token); !errors.Is(err, authn.ErrInvalidSession) {
		t.Fatalf("ResolveSession() after revoke error = %v", err)
	}
	commandTag, err := pool.Exec(ctx, `
		DELETE FROM radishnexus.user_sessions WHERE id = 'ses_integration'
	`)
	if err != nil || commandTag.RowsAffected() != 1 {
		t.Fatalf("delete revoked user session = %d, %v", commandTag.RowsAffected(), err)
	}
}

func integrationToken(value byte) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{value}, 32))
}
