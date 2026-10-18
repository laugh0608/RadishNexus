//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const recoveryOldPassword = "original controlled recovery password"
const recoveryNewPassword = "replacement controlled recovery password"
const recoveryEmail = "recovery@example.test"

type recoveryFixture struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	store   *authpostgres.Store
	auth    *authn.Service
	service *authn.RecoveryService
	owner   authn.BootstrapResult
	session authn.Session
}

func newIdentityTestDatabase(t *testing.T) *pgxpool.Pool {
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
	name := fmt.Sprintf("nexus_recovery_%d", time.Now().UnixNano())
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
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Migrate(ctx, conn.Conn())
	conn.Release()
	if err != nil {
		t.Fatal(err)
	}
	return pool
}
func newRecoveryFixture(t *testing.T) *recoveryFixture {
	t.Helper()
	ctx := context.Background()
	pool := newIdentityTestDatabase(t)
	store := authpostgres.New(pool)
	h := authn.NewArgon2idHasher()
	a := authn.NewService(store, h, authn.CryptoSecretGenerator{}, authn.SystemClock{})
	owner, err := a.Bootstrap(ctx, authn.BootstrapInput{Email: recoveryEmail, Password: recoveryOldPassword, DisplayName: "Recovery Owner", WorkspaceName: "Recovery Workspace"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := a.Login(ctx, authn.LoginInput{Email: recoveryEmail, Password: recoveryOldPassword})
	if err != nil {
		t.Fatal(err)
	}
	return &recoveryFixture{ctx, pool, store, a, authn.NewRecoveryService(store, h, authn.CryptoSecretGenerator{}), owner, session}
}
func (f *recoveryFixture) input(t *testing.T, n int) authn.RecoveryInput {
	t.Helper()
	i, err := f.service.Inspect(f.ctx, recoveryEmail)
	if err != nil {
		t.Fatal(err)
	}
	return authn.RecoveryInput{UserID: i.UserID, ExpectedEmail: recoveryEmail, ExpectedPasswordChangedAt: i.PasswordChangedAt, OperationID: fmt.Sprintf("12345678-1234-4234-8234-%012d", n), OperatorRef: "ops-test-42", ReasonCode: "lost_local_password", NewPassword: recoveryNewPassword, Confirmed: true}
}
func (f *recoveryFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, append([]any{pgx.QueryExecModeSimpleProtocol}, args...)...); err != nil {
		t.Fatal(err)
	}
}
func (f *recoveryFixture) snapshot(t *testing.T, exclude ...string) map[string]string {
	t.Helper()
	rows, err := f.pool.Query(f.ctx, `SELECT tablename FROM pg_tables WHERE schemaname='radishnexus' ORDER BY tablename`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err = rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	rows.Close()
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	result := map[string]string{}
	for _, table := range tables {
		skip := false
		for _, x := range exclude {
			if x == table {
				skip = true
			}
		}
		if skip {
			continue
		}
		var data string
		if err = f.pool.QueryRow(f.ctx, `SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]'::jsonb)::text FROM `+pgx.Identifier{"radishnexus", table}.Sanitize()+` r`).Scan(&data); err != nil {
			t.Fatal(err)
		}
		result[table] = data
	}
	return result
}
func TestRecoveryAtomicSuccessAndHistoricalStatus(t *testing.T) {
	f := newRecoveryFixture(t)
	// A second normal local identity and outstanding admission capability are
	// created through the supported invitation API, never credential SQL.
	identity := authn.NewIdentityService(f.store, f.auth, "")
	invite, err := identity.CreateInvitation(f.ctx, f.session.Token, f.owner.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: invite.Token, Email: "member@example.test", Password: recoveryOldPassword, DisplayName: "Member"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = identity.CreateInvitation(f.ctx, f.session.Token, f.owner.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	// Foundation authorization fixture: same global user in multiple workspaces,
	// including a suspended membership. Recovery must leave all of it intact.
	f.exec(t, `INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_recovery_second','Second'),('wrk_recovery_suspended','Suspended'); INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,status,role) VALUES('wrk_recovery_second',$1,'active','member'),('wrk_recovery_suspended',$1,'suspended','member')`, f.owner.UserID)
	configuration := goldenpath.NewConfigurationService(goldenpostgres.New(f.pool), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	invocation := goldenpath.Invocation{Principal: authz.Principal{Kind: authz.PrincipalUser, ID: f.owner.UserID, WorkspaceID: f.owner.WorkspaceID}, SourceKind: "web", CorrelationID: "recovery-permissions"}
	configure := func(in goldenpath.ConfigurationInput) goldenpath.ConfigurationResult {
		t.Helper()
		r, e := configuration.Configure(f.ctx, invocation, in)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	team := configure(goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: f.owner.WorkspaceID, ClientOperationID: "recovery-team", Name: "Recovery Team"})
	project := configure(goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: f.owner.WorkspaceID, ClientOperationID: "recovery-project", Name: "Private Project", Key: "recovery", OwnerTeamID: team.Object.ID, InitialAdminUserID: f.owner.UserID, Visibility: "restricted"})
	configure(goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: project.Object.ID, ClientOperationID: "recovery-channel", Name: "Private Channel", Visibility: "restricted", MemberUserIDs: []string{f.owner.UserID}})
	sessions := []authn.Session{f.session}
	for range 2 {
		s, e := f.auth.Login(f.ctx, authn.LoginInput{Email: recoveryEmail, Password: recoveryOldPassword})
		if e != nil {
			t.Fatal(e)
		}
		sessions = append(sessions, s)
	}
	// Exercise pre-existing revoked rows and future-created session constraint.
	if err = f.auth.RevokeSession(f.ctx, sessions[2].Token, sessions[2].CSRFToken); err != nil {
		t.Fatal(err)
	}
	futureAuth := authn.NewService(f.store, authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, integrationClock{now: time.Now().Add(time.Minute)})
	future, e := futureAuth.Login(f.ctx, authn.LoginInput{Email: recoveryEmail, Password: recoveryOldPassword})
	if e != nil {
		t.Fatal(e)
	}
	sessions = append(sessions, future)
	for range authn.LoginFailureLimit {
		_, _ = f.auth.Login(f.ctx, authn.LoginInput{Email: recoveryEmail, Password: "wrong"})
	}
	baseline := f.snapshot(t, "local_credentials", "user_sessions", "identity_audit")
	var immutableCredential string
	if err = f.pool.QueryRow(f.ctx, `SELECT (to_jsonb(c)-'password_hash'-'password_changed_at'-'failed_login_count'-'locked_until')::text FROM radishnexus.local_credentials c WHERE user_id=$1`, f.owner.UserID).Scan(&immutableCredential); err != nil {
		t.Fatal(err)
	}
	const otherIdentitySQL = `SELECT jsonb_build_object(
  'credentials',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY user_id),'[]') FROM radishnexus.local_credentials c WHERE user_id<>$1),
  'sessions',(SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY id),'[]') FROM radishnexus.user_sessions s WHERE user_id<>$1),
  'old_audit',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY id),'[]') FROM radishnexus.identity_audit a WHERE action<>'local.password.recovered'))::text`
	var otherIdentityBefore string
	if err = f.pool.QueryRow(f.ctx, otherIdentitySQL, f.owner.UserID).Scan(&otherIdentityBefore); err != nil {
		t.Fatal(err)
	}
	input := f.input(t, 1)
	result, err := f.service.Apply(f.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Result != "applied" || result.UserID != f.owner.UserID {
		t.Fatal("wrong result")
	}
	if !reflect.DeepEqual(baseline, f.snapshot(t, "local_credentials", "user_sessions", "identity_audit")) {
		t.Fatal("recovery changed business or authorization state")
	}
	var otherIdentityAfter string
	if err = f.pool.QueryRow(f.ctx, otherIdentitySQL, f.owner.UserID).Scan(&otherIdentityAfter); err != nil || otherIdentityBefore != otherIdentityAfter {
		t.Fatal("non-target identity or old audit changed", err)
	}

	var after string
	var count int
	var locked *time.Time
	if err = f.pool.QueryRow(f.ctx, `SELECT (to_jsonb(c)-'password_hash'-'password_changed_at'-'failed_login_count'-'locked_until')::text,failed_login_count,locked_until FROM radishnexus.local_credentials c WHERE user_id=$1`, f.owner.UserID).Scan(&after, &count, &locked); err != nil || after != immutableCredential || count != 0 || locked != nil {
		t.Fatal("immutable credential or lock changed", err)
	}
	for _, s := range sessions {
		if _, err = f.auth.ResolveSession(f.ctx, s.Token); !errors.Is(err, authn.ErrInvalidSession) {
			t.Fatal("old session live", err)
		}
		if err = f.auth.VerifyCSRF(f.ctx, s.Token, s.CSRFToken); !errors.Is(err, authn.ErrInvalidSession) {
			t.Fatal("old CSRF live", err)
		}
	}
	if _, err = f.auth.ResolveSession(f.ctx, member.Token); err != nil {
		t.Fatal("other user session revoked", err)
	}
	if _, err = f.auth.Login(f.ctx, authn.LoginInput{Email: recoveryEmail, Password: recoveryOldPassword}); !errors.Is(err, authn.ErrInvalidCredentials) {
		t.Fatal("old password accepted", err)
	}
	fresh, err := f.auth.Login(f.ctx, authn.LoginInput{Email: recoveryEmail, Password: recoveryNewPassword})
	if err != nil || fresh.Account.User.ID != f.owner.UserID || len(fresh.Account.Workspaces) != 2 {
		t.Fatal("new login", err)
	}
	if _, err = f.auth.Bootstrap(f.ctx, authn.BootstrapInput{Email: "again@example.test", Password: recoveryOldPassword, DisplayName: "Again", WorkspaceName: "Again"}); !errors.Is(err, authn.ErrAlreadyBootstrapped) {
		t.Fatal("bootstrap reopened", err)
	}
	var valid bool
	if err = f.pool.QueryRow(f.ctx, `SELECT actor_kind='operator' AND actor_id IS NULL AND workspace_id IS NULL AND operator_ref=$2 AND target_user_id=$1 AND previous_password_changed_at=$3 AND applied_password_changed_at=occurred_at FROM radishnexus.identity_audit WHERE operation_id=$4`, f.owner.UserID, input.OperatorRef, input.ExpectedPasswordChangedAt, input.OperationID).Scan(&valid); err != nil || !valid {
		t.Fatal("bad operator audit", err)
	}
	for _, sql := range []string{`UPDATE radishnexus.identity_audit SET operator_ref='changed' WHERE operation_id=$1`, `DELETE FROM radishnexus.identity_audit WHERE operation_id=$1`, `UPDATE radishnexus.identity_audit SET action='local.password.recovered' WHERE action='invitation.created' AND $1::uuid IS NOT NULL`} {
		if _, err = f.pool.Exec(f.ctx, sql, input.OperationID); err == nil {
			t.Fatal("mutable recovery audit")
		}
	}
	for _, user := range []string{f.owner.UserID, member.Account.User.ID} {
		retry := input
		retry.UserID = user
		retry.NewPassword = "different repeated password"
		if _, err = f.service.Apply(f.ctx, retry); !errors.Is(err, authn.ErrRecoveryRecorded) {
			t.Fatal("reused operation", err)
		}
	}
	second := f.input(t, 2)
	second.NewPassword = "another replacement password"
	if _, err = f.service.Apply(f.ctx, second); err != nil {
		t.Fatal(err)
	}
	historical, err := f.service.Status(f.ctx, input.UserID, input.OperationID)
	if err != nil || historical != result {
		t.Fatal("historical operation changed", err)
	}
	if _, err = f.auth.Login(f.ctx, authn.LoginInput{Email: recoveryEmail, Password: second.NewPassword}); err != nil {
		t.Fatal(err)
	}
	cross, err := f.service.Status(f.ctx, member.Account.User.ID, input.OperationID)
	if err != nil || cross.Result != "not_observed" || cross.AuditID != "" {
		t.Fatal("cross user status disclosure")
	}
}

func TestRecoveryRejectedAndRollbackSnapshots(t *testing.T) {
	for _, scenario := range []string{"disabled account", "disabled credential", "missing", "OIDC only", "legacy", "mismatch", "same password", "corrupt verifier", "clock", "credential failure", "session failure", "audit failure", "deferred commit failure", "readonly", "lock timeout", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			f := newRecoveryFixture(t)
			input := f.input(t, 1)
			switch scenario {
			case "disabled account":
				f.exec(t, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, f.owner.UserID)
			case "disabled credential":
				f.exec(t, `UPDATE radishnexus.local_credentials SET status='disabled' WHERE user_id=$1`, f.owner.UserID)
			case "missing":
				input.UserID = "usr_missing"
			case "OIDC only":
				f.exec(t, `INSERT INTO radishnexus.users(id,display_name) VALUES('usr_oidc_only','OIDC'); INSERT INTO radishnexus.user_accounts(user_id,status,created_at) VALUES('usr_oidc_only','active',clock_timestamp()); INSERT INTO radishnexus.external_identities(user_id,issuer,subject,created_at) VALUES('usr_oidc_only','https://id.example.test','subject',clock_timestamp())`)
				input.UserID = "usr_oidc_only"
			case "legacy":
				f.exec(t, `INSERT INTO radishnexus.users(id,display_name) VALUES('usr_legacy','Legacy'); INSERT INTO radishnexus.user_accounts(user_id,status,created_at) VALUES('usr_legacy','active',clock_timestamp()); INSERT INTO radishnexus.local_credentials(user_id,legacy_login_name,password_hash,status,created_at,password_changed_at) VALUES('usr_legacy','legacy','damaged','active',clock_timestamp(),clock_timestamp())`)
				input.UserID = "usr_legacy"
			case "mismatch":
				input.ExpectedEmail = "other@example.test"
			case "same password":
				input.NewPassword = recoveryOldPassword
			case "corrupt verifier":
				f.exec(t, `UPDATE radishnexus.local_credentials SET password_hash='corrupt',password_changed_at=clock_timestamp() WHERE user_id=$1`, f.owner.UserID)
				input = f.input(t, 1)
			case "clock":
				f.exec(t, `UPDATE radishnexus.local_credentials SET password_changed_at=clock_timestamp()+interval '1 hour' WHERE user_id=$1`, f.owner.UserID)
				input = f.input(t, 1)
			case "credential failure", "session failure", "audit failure", "deferred commit failure":
				f.exec(t, `CREATE FUNCTION radishnexus.fail_recovery_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private constraint marker' USING ERRCODE='23514'; END $$`)
				sql := `CREATE TRIGGER fail_test AFTER UPDATE ON radishnexus.local_credentials FOR EACH ROW EXECUTE FUNCTION radishnexus.fail_recovery_test()`
				if scenario == "session failure" {
					sql = `CREATE TRIGGER fail_test AFTER UPDATE ON radishnexus.user_sessions FOR EACH ROW EXECUTE FUNCTION radishnexus.fail_recovery_test()`
				}
				if scenario == "audit failure" {
					sql = `CREATE TRIGGER fail_test AFTER INSERT ON radishnexus.identity_audit FOR EACH ROW EXECUTE FUNCTION radishnexus.fail_recovery_test()`
				}
				if scenario == "deferred commit failure" {
					sql = `CREATE CONSTRAINT TRIGGER fail_test AFTER INSERT ON radishnexus.identity_audit DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.fail_recovery_test()`
				}
				f.exec(t, sql)
			}
			service := f.service
			ctx := f.ctx
			if scenario == "readonly" {
				config := f.pool.Config().Copy()
				config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
				pool, err := pgxpool.NewWithConfig(ctx, config)
				if err != nil {
					t.Fatal(err)
				}
				defer pool.Close()
				service = authn.NewRecoveryService(authpostgres.New(pool), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{})
			}
			if scenario == "lock timeout" {
				tx, err := f.pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				if _, err = tx.Exec(ctx, `SELECT 1 FROM radishnexus.user_accounts WHERE user_id=$1 FOR UPDATE`, f.owner.UserID); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "canceled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			before := f.snapshot(t)
			_, err := service.Apply(ctx, input)
			if err == nil {
				t.Fatal("expected rejection")
			}
			if errors.Is(err, authn.ErrRecoveryUnconfirmed) {
				t.Fatal("definite failure reported unknown", err)
			}
			if strings.Contains(err.Error(), "private constraint marker") {
				t.Fatal("SQL detail leaked")
			}
			if !reflect.DeepEqual(before, f.snapshot(t)) {
				t.Fatal("partial recovery after failure")
			}
		})
	}
}

// The verifier barrier pauses after the original credential has been checked,
// before a login writes a Session or failed-attempt counter.
type recoveryBarrierHasher struct {
	authn.PasswordHasher
	entered chan struct{}
	release chan struct{}
}

func (h recoveryBarrierHasher) Verify(hash, password string) (bool, error) {
	ok, err := h.PasswordHasher.Verify(hash, password)
	h.entered <- struct{}{}
	<-h.release
	return ok, err
}
func TestRecoveryConcurrentOldLoginAndFailedAttempts(t *testing.T) {
	for _, password := range []string{recoveryOldPassword, "wrong password"} {
		t.Run(password, func(t *testing.T) {
			f := newRecoveryFixture(t)
			barrier := recoveryBarrierHasher{authn.NewArgon2idHasher(), make(chan struct{}, 1), make(chan struct{})}
			delayed := authn.NewService(f.store, barrier, authn.CryptoSecretGenerator{}, authn.SystemClock{})
			outcome := make(chan error, 1)
			go func() {
				_, err := delayed.Login(f.ctx, authn.LoginInput{Email: recoveryEmail, Password: password})
				outcome <- err
			}()
			<-barrier.entered
			if _, err := f.service.Apply(f.ctx, f.input(t, 1)); err != nil {
				close(barrier.release)
				t.Fatal(err)
			}
			close(barrier.release)
			if err := <-outcome; !errors.Is(err, authn.ErrInvalidCredentials) {
				t.Fatal("stale login succeeded", err)
			}
			var count, live int
			if err := f.pool.QueryRow(f.ctx, `SELECT failed_login_count FROM radishnexus.local_credentials WHERE user_id=$1`, f.owner.UserID).Scan(&count); err != nil || count != 0 {
				t.Fatal("stale failure changed new counter", err)
			}
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM radishnexus.user_sessions WHERE user_id=$1 AND revoked_at IS NULL`, f.owner.UserID).Scan(&live); err != nil || live != 0 {
				t.Fatal("stale session survived", err)
			}
			for range authn.LoginFailureLimit {
				if _, err := f.auth.Login(f.ctx, authn.LoginInput{Email: recoveryEmail, Password: "wrong password"}); !errors.Is(err, authn.ErrInvalidCredentials) {
					t.Fatal(err)
				}
			}
			if _, err := f.auth.Login(f.ctx, authn.LoginInput{Email: recoveryEmail, Password: recoveryNewPassword}); !errors.Is(err, authn.ErrInvalidCredentials) {
				t.Fatal("new failures did not lock", err)
			}
		})
	}
}
func TestRecoveryConcurrentOperations(t *testing.T) {
	for _, sameID := range []bool{false, true} {
		t.Run(fmt.Sprint(sameID), func(t *testing.T) {
			f := newRecoveryFixture(t)
			a, b := f.input(t, 1), f.input(t, 2)
			if sameID {
				b.OperationID = a.OperationID
			}
			b.NewPassword = "concurrent second replacement password"
			barrier := recoveryBarrierHasher{authn.NewArgon2idHasher(), make(chan struct{}, 2), make(chan struct{})}
			s := authn.NewRecoveryService(f.store, barrier, authn.CryptoSecretGenerator{})
			outcomes := make(chan error, 2)
			for _, input := range []authn.RecoveryInput{a, b} {
				go func() { _, err := s.Apply(f.ctx, input); outcomes <- err }()
			}
			<-barrier.entered
			<-barrier.entered
			close(barrier.release)
			wins := 0
			for range 2 {
				err := <-outcomes
				if err == nil {
					wins++
				} else if !errors.Is(err, authn.ErrRecoveryConflict) && !errors.Is(err, authn.ErrRecoveryRecorded) {
					t.Fatal(err)
				}
			}
			if wins != 1 {
				t.Fatal("multiple winners")
			}
			var count int
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM radishnexus.identity_audit WHERE action='local.password.recovered'`).Scan(&count); err != nil || count != 1 {
				t.Fatal("audit count", err)
			}
		})
	}
}
func TestRecoveryLoginFirstAndConcurrentDisable(t *testing.T) {
	f := newRecoveryFixture(t)
	// A transaction barrier makes login acquire the account lock before recovery.
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, `SELECT 1 FROM radishnexus.user_accounts WHERE user_id=$1 FOR UPDATE`, f.owner.UserID); err != nil {
		t.Fatal(err)
	}
	c, err := f.store.FindLocalAccount(f.ctx, recoveryEmail)
	if err != nil {
		t.Fatal(err)
	}
	// Complete a real login while the recovered operation is paused in KDF;
	// successful sessions necessarily commit before recovery can acquire its lock.
	barrier := recoveryBarrierHasher{authn.NewArgon2idHasher(), make(chan struct{}, 1), make(chan struct{})}
	service := authn.NewRecoveryService(f.store, barrier, authn.CryptoSecretGenerator{})
	input := f.input(t, 1)
	recovered := make(chan error, 1)
	go func() { _, e := service.Apply(f.ctx, input); recovered <- e }()
	<-barrier.entered
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	login, err := f.auth.Login(f.ctx, authn.LoginInput{Email: recoveryEmail, Password: recoveryOldPassword})
	if err != nil {
		t.Fatal(err)
	}
	close(barrier.release)
	if err = <-recovered; err != nil {
		t.Fatal(err)
	}
	if _, err = f.auth.ResolveSession(f.ctx, login.Token); !errors.Is(err, authn.ErrInvalidSession) {
		t.Fatal("login-first session survived", err)
	}
	if err = f.store.RecordFailedLogin(f.ctx, c, time.Now(), time.Now().Add(time.Minute), 1); err != nil {
		t.Fatal(err)
	}
	// Disable after preparation, before the recovery locks current state.
	barrier = recoveryBarrierHasher{authn.NewArgon2idHasher(), make(chan struct{}, 1), make(chan struct{})}
	service = authn.NewRecoveryService(f.store, barrier, authn.CryptoSecretGenerator{})
	input = f.input(t, 2)
	input.NewPassword = "third controlled recovery password"
	go func() { _, e := service.Apply(f.ctx, input); recovered <- e }()
	<-barrier.entered
	f.exec(t, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, f.owner.UserID)
	before := f.snapshot(t)
	close(barrier.release)
	if err = <-recovered; !errors.Is(err, authn.ErrRecoveryIneligible) {
		t.Fatal("disabled account recovered", err)
	}
	if !reflect.DeepEqual(before, f.snapshot(t)) {
		t.Fatal("disabled account mutated")
	}
}

func tokenHash(token string) []byte { sum := sha256.Sum256([]byte(token)); return sum[:] }

func TestRecoveryBindingAndUnlinkInterleavings(t *testing.T) {
	for _, first := range []string{"binding", "recovery", "unlink"} {
		t.Run(first, func(t *testing.T) {
			f := newRecoveryFixture(t)
			now := time.Now().UTC()
			binding := authn.OIDCCompletion{Identity: authn.ExternalIdentity{Issuer: "https://id.example.test", Subject: "recovery-subject"}, Transaction: authn.OIDCTransaction{InitiatingSessionDigest: tokenHash(f.session.Token), ExpiresAt: now.Add(time.Minute)}, Session: authn.SessionRecord{CreatedAt: now}, AuditID: "ida_bind"}
			if first == "binding" || first == "unlink" {
				if err := f.store.CompleteOIDC(f.ctx, binding); err != nil {
					t.Fatal(err)
				}
			}
			input := f.input(t, 1)
			if first == "unlink" {
				barrier := recoveryBarrierHasher{authn.NewArgon2idHasher(), make(chan struct{}, 1), make(chan struct{})}
				s := authn.NewRecoveryService(f.store, barrier, authn.CryptoSecretGenerator{})
				done := make(chan error, 1)
				go func() { _, err := s.Apply(f.ctx, input); done <- err }()
				<-barrier.entered
				err := f.store.UnlinkExternal(f.ctx, tokenHash(f.session.Token), binding.Identity.Issuer, "ida_unlink", time.Now())
				close(barrier.release)
				if err != nil {
					t.Fatal(err)
				}
				if err = <-done; err != nil {
					t.Fatal(err)
				}
				var exists bool
				if err = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM radishnexus.external_identities)`).Scan(&exists); err != nil || exists {
					t.Fatal("recovery restored removed binding", err)
				}
				return
			}
			if _, err := f.service.Apply(f.ctx, input); err != nil {
				t.Fatal(err)
			}
			binding.AuditID = "ida_old_callback"
			if err := f.store.CompleteOIDC(f.ctx, binding); !errors.Is(err, authn.ErrInvalidSession) {
				t.Fatal("old binding session survived", err)
			}
			if first == "binding" {
				// Fresh valid provider identity remains usable; this is Store coverage only.
				login := binding
				login.Transaction.InitiatingSessionDigest = nil
				login.AuditID = "ida_external_login"
				login.Session = authn.SessionRecord{ID: "ses_external_recovered", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(authn.SessionLifetime), TokenDigest: tokenHash("external login token"), CSRFTokenDigest: tokenHash("external csrf token")}
				if err := f.store.CompleteOIDC(f.ctx, login); err != nil {
					t.Fatal(err)
				}
				if _, err := f.store.ResolveSession(f.ctx, login.Session.TokenDigest, time.Now()); err != nil {
					t.Fatal("new external login blocked", err)
				}
				if err := f.store.UnlinkExternal(f.ctx, tokenHash(f.session.Token), binding.Identity.Issuer, "ida_old_unlink", time.Now()); !errors.Is(err, authn.ErrInvalidSession) {
					t.Fatal("old unlink authorized", err)
				}
			}
		})
	}
}

func TestRecoveryStatusDuringUncommittedOperation(t *testing.T) {
	f := newRecoveryFixture(t)
	input := f.input(t, 1)
	lock, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(f.ctx)
	// SHARE allows the operation/status reads but blocks its final audit INSERT.
	if _, err = lock.Exec(f.ctx, `LOCK TABLE radishnexus.identity_audit IN SHARE MODE`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, e := f.service.Apply(f.ctx, input); done <- e }()
	deadline := time.Now().Add(4 * time.Second)
	for {
		var blocked bool
		err = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'INSERT INTO radishnexus.identity_audit%')`).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("operation did not reach insert barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	status, err := f.service.Status(f.ctx, input.UserID, input.OperationID)
	if err != nil || status.Result != "not_observed" {
		t.Fatal("inflight operation reported committed", err)
	}
	if err = lock.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	status, err = f.service.Status(f.ctx, input.UserID, input.OperationID)
	if err != nil || status.Result != "applied" {
		t.Fatal("commit not observable", err)
	}
	if _, err = f.service.Apply(f.ctx, input); !errors.Is(err, authn.ErrRecoveryRecorded) {
		t.Fatal("retry after not_observed overwrote", err)
	}
}

func TestRecoveryCancellationAfterWritesAndDeadlockRollback(t *testing.T) {
	for _, scenario := range []string{"cancel after audit insert", "deadlock after credential update"} {
		t.Run(scenario, func(t *testing.T) {
			f := newRecoveryFixture(t)
			input := f.input(t, 1)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			service := f.service
			var blocker pgx.Tx
			if scenario == "cancel after audit insert" {
				f.exec(t, `CREATE FUNCTION radishnexus.pause_recovery_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(20); RETURN NEW; END $$; CREATE TRIGGER pause_test AFTER INSERT ON radishnexus.identity_audit FOR EACH ROW EXECUTE FUNCTION radishnexus.pause_recovery_test()`)
			} else {
				var err error
				blocker, err = f.pool.Begin(f.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer blocker.Rollback(f.ctx)
				if _, err = blocker.Exec(f.ctx, `SELECT 1 FROM radishnexus.user_sessions WHERE user_id=$1 FOR UPDATE`, f.owner.UserID); err != nil {
					t.Fatal(err)
				}
				config := f.pool.Config().Copy()
				config.ConnConfig.RuntimeParams["deadlock_timeout"] = "100ms"
				pool, err := pgxpool.NewWithConfig(f.ctx, config)
				if err != nil {
					t.Fatal(err)
				}
				defer pool.Close()
				service = authn.NewRecoveryService(authpostgres.New(pool), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{})
			}
			before := f.snapshot(t)
			done := make(chan error, 1)
			go func() { _, err := service.Apply(ctx, input); done <- err }()
			waiting := `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event='PgSleep')`
			if blocker != nil {
				waiting = `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'UPDATE radishnexus.user_sessions SET revoked_at=greatest%')`
			}
			deadline := time.Now().Add(4 * time.Second)
			for {
				var reached bool
				if err := f.pool.QueryRow(f.ctx, waiting).Scan(&reached); err != nil {
					t.Fatal(err)
				}
				if reached {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("write barrier not reached")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if blocker == nil {
				cancel()
			} else {
				if _, err := blocker.Exec(f.ctx, `SELECT 1 FROM radishnexus.user_accounts WHERE user_id=$1 FOR UPDATE`, f.owner.UserID); err != nil {
					t.Fatal("unexpected deadlock victim", err)
				}
				if err := blocker.Rollback(f.ctx); err != nil {
					t.Fatal(err)
				}
			}
			err := <-done
			if err == nil || errors.Is(err, authn.ErrRecoveryUnconfirmed) {
				t.Fatal("write failure classification", err)
			}
			if blocker != nil {
				var state interface{ SQLState() string }
				if !errors.As(err, &state) || state.SQLState() != "40P01" {
					t.Fatal("expected PostgreSQL deadlock", err)
				}
			}
			if !reflect.DeepEqual(before, f.snapshot(t)) {
				t.Fatal("partial state after canceled/deadlock transaction")
			}
		})
	}
}
func TestRecoverySameOperationAcrossConcurrentUsers(t *testing.T) {
	f := newRecoveryFixture(t)
	identity := authn.NewIdentityService(f.store, f.auth, "")
	invite, err := identity.CreateInvitation(f.ctx, f.session.Token, f.owner.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: invite.Token, Email: "second-recovery@example.test", Password: recoveryOldPassword, DisplayName: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	a := f.input(t, 1)
	inspection, err := f.service.Inspect(f.ctx, "second-recovery@example.test")
	if err != nil {
		t.Fatal(err)
	}
	b := a
	b.UserID = member.Account.User.ID
	b.ExpectedEmail = "second-recovery@example.test"
	b.ExpectedPasswordChangedAt = inspection.PasswordChangedAt
	barrier := recoveryBarrierHasher{authn.NewArgon2idHasher(), make(chan struct{}, 2), make(chan struct{})}
	service := authn.NewRecoveryService(f.store, barrier, authn.CryptoSecretGenerator{})
	done := make(chan error, 2)
	for _, in := range []authn.RecoveryInput{a, b} {
		go func() { _, e := service.Apply(f.ctx, in); done <- e }()
	}
	<-barrier.entered
	<-barrier.entered
	close(barrier.release)
	wins := 0
	for range 2 {
		e := <-done
		if e == nil {
			wins++
		} else if !errors.Is(e, authn.ErrRecoveryRecorded) {
			t.Fatal("wrong duplicate result", e)
		}
	}
	if wins != 1 {
		t.Fatal("operation identity not globally unique")
	}
}

func TestRecoveryOrdinaryMemberKeepsRole(t *testing.T) {
	f := newRecoveryFixture(t)
	identity := authn.NewIdentityService(f.store, f.auth, "")
	invite, err := identity.CreateInvitation(f.ctx, f.session.Token, f.owner.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: invite.Token, Email: "ordinary@example.test", Password: recoveryOldPassword, DisplayName: "Ordinary"})
	if err != nil {
		t.Fatal(err)
	}
	inspected, err := f.service.Inspect(f.ctx, "ordinary@example.test")
	if err != nil {
		t.Fatal(err)
	}
	input := f.input(t, 1)
	input.UserID = member.Account.User.ID
	input.ExpectedEmail = "ordinary@example.test"
	input.ExpectedPasswordChangedAt = inspected.PasswordChangedAt
	if _, err = f.service.Apply(f.ctx, input); err != nil {
		t.Fatal(err)
	}
	fresh, err := f.auth.Login(f.ctx, authn.LoginInput{Email: input.ExpectedEmail, Password: input.NewPassword})
	if err != nil || fresh.Account.User.ID != member.Account.User.ID || len(fresh.Account.Workspaces) != 1 || fresh.Account.Workspaces[0].Role != "member" {
		t.Fatal("ordinary member recovery changed role", err)
	}
	if _, err = f.auth.Login(f.ctx, authn.LoginInput{Email: input.ExpectedEmail, Password: recoveryOldPassword}); !errors.Is(err, authn.ErrInvalidCredentials) {
		t.Fatal("ordinary old password accepted", err)
	}
	if _, err = f.auth.ResolveSession(f.ctx, member.Token); !errors.Is(err, authn.ErrInvalidSession) {
		t.Fatal("ordinary old session survived", err)
	}
	if _, err = f.auth.ResolveSession(f.ctx, f.session.Token); err != nil {
		t.Fatal("owner session changed", err)
	}
}
