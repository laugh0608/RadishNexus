//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
)

type handoffFixture struct {
	*recoveryFixture
	identity *authn.IdentityService
	transfer *goldenpath.OwnerTransferService
	members  []authn.Session
}

func newHandoffFixture(t *testing.T, count int) *handoffFixture {
	t.Helper()
	f := newRecoveryFixture(t)
	h := &handoffFixture{recoveryFixture: f, identity: authn.NewIdentityService(f.store, f.auth, ""), transfer: goldenpath.NewOwnerTransferService(goldenpostgres.New(f.pool), goldenpath.CryptoIDGenerator{})}
	for n := range count {
		invite, e := h.identity.CreateInvitation(f.ctx, f.session.Token, f.owner.WorkspaceID)
		if e != nil {
			t.Fatal(e)
		}
		email := fmt.Sprintf("handoff-%d@example.test", n)
		if _, e = h.identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: invite.Token, Email: email, DisplayName: "Same Name", Password: recoveryOldPassword}); e != nil {
			t.Fatal(e)
		}
		s, e := f.auth.Login(f.ctx, authn.LoginInput{Email: email, Password: recoveryOldPassword})
		if e != nil {
			t.Fatal(e)
		}
		h.members = append(h.members, s)
	}
	return h
}
func (f *handoffFixture) principal(s authn.Session) authz.Principal {
	return authz.Principal{Kind: authz.PrincipalUser, ID: s.Account.User.ID, WorkspaceID: f.owner.WorkspaceID}
}
func handoffInput(target string, n int) goldenpath.OwnerTransferInput {
	return goldenpath.OwnerTransferInput{ClientOperationID: fmt.Sprintf("12345678-1234-4234-8234-%012d", n), TargetUserID: target, ExpectedActorRoleRevision: "1", ExpectedTargetRoleRevision: "1", Confirmed: true}
}
func (f *handoffFixture) apply(ctx context.Context, s authn.Session, in goldenpath.OwnerTransferInput) (goldenpath.OwnerTransferResult, error) {
	out, err := f.transfer.Transfer(ctx, goldenpath.Invocation{Principal: f.principal(s), SourceKind: "web", CorrelationID: "handoff-test"}, s.Token, in)
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) {
		err = errors.Join(err, fmt.Errorf("test database diagnostic: code=%s constraint=%s message=%s", pgerr.Code, pgerr.ConstraintName, pgerr.Message))
	}
	return out, err
}
func (f *handoffFixture) state(t *testing.T, s authn.Session) (string, int64) {
	t.Helper()
	var role string
	var rev int64
	if e := f.pool.QueryRow(f.ctx, `SELECT role,role_revision FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2`, f.owner.WorkspaceID, s.Account.User.ID).Scan(&role, &rev); e != nil {
		t.Fatal(e)
	}
	return role, rev
}
func (f *handoffFixture) count(t *testing.T, table string, want int) {
	t.Helper()
	var n int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM radishnexus.`+table+` WHERE command_kind='workspace.owner.transfer'`).Scan(&n); e != nil || n != want {
		t.Fatalf("%s count=%d want=%d error=%v", table, n, want, e)
	}
}
func (f *handoffFixture) wait(t *testing.T, pattern string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for {
		var yes bool
		if e := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE $1)`, pattern).Scan(&yes); e != nil {
			t.Fatal(e)
		}
		if yes {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("lock barrier not reached", pattern)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func (f *handoffFixture) gate(t *testing.T, sql string, args ...any) pgx.Tx {
	t.Helper()
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, e = tx.Exec(f.ctx, sql, args...); e != nil {
		t.Fatal(e)
	}
	return tx
}
func TestOwnerTransferAtomicTenureAndHistoricalRetry(t *testing.T) {
	f := newHandoffFixture(t, 2)
	a, b, c := f.session, f.members[0], f.members[1]
	// A formal private project retains its administrator after Workspace demotion.
	config := goldenpath.NewConfigurationService(goldenpostgres.New(f.pool), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	call := func(s authn.Session, in goldenpath.ConfigurationInput) (goldenpath.ConfigurationResult, error) {
		return config.Configure(f.ctx, goldenpath.Invocation{Principal: f.principal(s), SourceKind: "web"}, in)
	}
	teamIn := goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: f.owner.WorkspaceID, ClientOperationID: "handoff-team", Name: "Team"}
	team, e := call(a, teamIn)
	if e != nil {
		t.Fatal(e)
	}
	project, e := call(a, goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: f.owner.WorkspaceID, ClientOperationID: "handoff-project", Name: "Private", Key: "handoff", OwnerTeamID: team.Object.ID, InitialAdminUserID: a.Account.User.ID, Visibility: "restricted"})
	if e != nil {
		t.Fatal(e)
	}
	// Explicit staging permission and private channel are independent of owner.
	component, e := call(a, goldenpath.ConfigurationInput{Kind: "component.create", ScopeID: f.owner.WorkspaceID, ClientOperationID: "handoff-component", Name: "Service", Key: "service", OwnerTeamID: team.Object.ID, Delivery: &goldenpath.DeliveryConfigurationInput{Type: "service"}})
	if e != nil {
		t.Fatal(e)
	}
	environment, e := call(a, goldenpath.ConfigurationInput{Kind: "environment.create", ScopeID: f.owner.WorkspaceID, ClientOperationID: "handoff-environment", Name: "Stage", Key: "stage", OwnerTeamID: team.Object.ID, Delivery: &goldenpath.DeliveryConfigurationInput{Classification: "staging"}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = call(a, goldenpath.ConfigurationInput{Kind: "environment.authorization.grant", ScopeID: environment.Object.ID, UserID: a.Account.User.ID, ClientOperationID: "handoff-grant", Delivery: &goldenpath.DeliveryConfigurationInput{Confirmed: true}}); e != nil {
		t.Fatal(e)
	}
	if _, e = call(a, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: project.Object.ID, ClientOperationID: "handoff-private-channel", Name: "Private", Visibility: "restricted", MemberUserIDs: []string{a.Account.User.ID}}); e != nil {
		t.Fatal(e)
	}
	core := goldenpath.NewService(goldenpostgres.New(f.pool), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	build, e := core.RecordCompletedJenkinsRun(f.ctx, goldenpath.VerifiedJenkinsDelivery{WorkspaceID: f.owner.WorkspaceID, SourceID: "handoff-source", DeliveryID: "handoff-build", PayloadSHA256: strings.Repeat("a", 64)}, goldenpath.RecordCompletedCIRunInput{ComponentID: component.Object.ID, ExternalRunKey: "handoff-build", Status: "succeeded", CompletedAt: time.Now().UTC().Truncate(time.Millisecond).Add(-time.Minute)})
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_other','Other'); INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,status,role) VALUES('wrk_other',$1,'active','owner'),('wrk_other',$2,'active','member')`, a.Account.User.ID, b.Account.User.ID)
	oldInvite, e := f.identity.CreateInvitation(f.ctx, a.Token, f.owner.WorkspaceID)
	if e != nil {
		t.Fatal(e)
	}
	before := f.snapshot(t, "workspace_memberships", "workspace_configuration_audit", "workspace_configuration_receipts")
	var otherBefore string
	_ = f.pool.QueryRow(f.ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY user_id)::text FROM radishnexus.workspace_memberships m WHERE workspace_id='wrk_other'`).Scan(&otherBefore)
	in := handoffInput(b.Account.User.ID, 1)
	out, e := f.apply(f.ctx, a, in)
	if e != nil {
		t.Fatal(e)
	}
	if out.Result != "applied" || out.ActorRoleRevision != 2 || out.TargetRoleRevision != 2 {
		t.Fatal(out)
	}
	if role, rev := f.state(t, a); role != "member" || rev != 2 {
		t.Fatal(role, rev)
	}
	if role, rev := f.state(t, b); role != "owner" || rev != 2 {
		t.Fatal(role, rev)
	}
	if !reflect.DeepEqual(before, f.snapshot(t, "workspace_memberships", "workspace_configuration_audit", "workspace_configuration_receipts")) {
		t.Fatal("unrelated persistent/ephemeral data changed")
	}
	var otherAfter string
	_ = f.pool.QueryRow(f.ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY user_id)::text FROM radishnexus.workspace_memberships m WHERE workspace_id='wrk_other'`).Scan(&otherAfter)
	if otherBefore != otherAfter {
		t.Fatal("other workspace changed")
	}
	for _, s := range []authn.Session{a, b} {
		o, e := f.transfer.Ownership(f.ctx, f.principal(s), s.Token)
		if e != nil {
			t.Fatal(e)
		}
		role, _ := f.state(t, s)
		if o.Role != role {
			t.Fatal(o)
		}
	}
	deployment := goldenpath.RecordStagingDeploymentInput{EnvironmentID: environment.Object.ID, CIRunID: build.CIRun.ID, Status: "succeeded", CompletedAt: time.Now().UTC().Truncate(time.Millisecond), ClientOperationID: "after-demotion", Confirmed: true}
	if _, e = core.RecordStagingDeployment(f.ctx, goldenpath.Invocation{Principal: f.principal(b), SourceKind: "web", CorrelationID: "handoff-grant-new-owner"}, deployment); !errors.Is(e, authz.ErrForbidden) {
		t.Fatal("new owner inherited Deployment grant", e)
	}
	if _, e = core.RecordStagingDeployment(f.ctx, goldenpath.Invocation{Principal: f.principal(a), SourceKind: "web", CorrelationID: "handoff-grant-former-owner"}, deployment); e != nil {
		t.Fatal("demotion removed explicit Deployment grant", e)
	}
	if _, e = call(a, teamIn); !errors.Is(e, authz.ErrForbidden) {
		t.Fatal("old config receipt bypassed owner", e)
	}
	if _, e = call(a, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: project.Object.ID, ClientOperationID: "after-demotion", Name: "Retained admin", Visibility: "project", MemberUserIDs: []string{}}); e != nil {
		t.Fatal("narrow admin lost", e)
	}
	if _, e = f.identity.CreateInvitation(f.ctx, a.Token, f.owner.WorkspaceID); !errors.Is(e, authz.ErrForbidden) {
		t.Fatal("old owner invitation", e)
	}
	if _, e = f.identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: oldInvite.Token, Email: "stale@example.test", Password: recoveryOldPassword, DisplayName: "Stale"}); !errors.Is(e, authn.ErrInvitationInvalid) {
		t.Fatal("old invitation", e)
	}
	back := handoffInput(a.Account.User.ID, 2)
	back.ExpectedActorRoleRevision = "2"
	back.ExpectedTargetRoleRevision = "2"
	if _, e = f.apply(f.ctx, b, back); e != nil {
		t.Fatal(e)
	}
	if _, e = f.identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: oldInvite.Token, Email: "stale@example.test", Password: recoveryOldPassword, DisplayName: "Stale"}); !errors.Is(e, authn.ErrInvitationInvalid) {
		t.Fatal("old tenure revived", e)
	}
	stale := in
	stale.ClientOperationID = handoffInput("", 3).ClientOperationID
	if _, e = f.apply(f.ctx, a, stale); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("ABA accepted", e)
	}
	// Historical success does not inspect the target's present account state.
	f.exec(t, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, b.Account.User.ID)
	f.exec(t, `ALTER TABLE radishnexus.user_sessions DISABLE TRIGGER user_sessions_control_mutation; UPDATE radishnexus.user_sessions SET created_at=clock_timestamp()-interval '11 minutes' WHERE token_digest=$1; ALTER TABLE radishnexus.user_sessions ENABLE TRIGGER user_sessions_control_mutation`, tokenHash(a.Token))
	historical, e := f.apply(f.ctx, a, in)
	if e != nil || historical.AuditID != out.AuditID {
		t.Fatal("history replay", e, historical)
	}
	status, e := f.transfer.Status(f.ctx, f.principal(a), a.Token, in.ClientOperationID)
	if e != nil || status.AuditID != out.AuditID {
		t.Fatal(e, status)
	}
	for _, s := range []authn.Session{c} {
		status, e = f.transfer.Status(f.ctx, f.principal(s), s.Token, in.ClientOperationID)
		if e != nil || status.Result != "not_observed" {
			t.Fatal(e, status)
		}
	}
	mismatch := in
	mismatch.TargetUserID = c.Account.User.ID
	if _, e = f.apply(f.ctx, a, mismatch); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("same operation changed target", e)
	}
	f.count(t, "workspace_configuration_audit", 2)
	f.count(t, "workspace_configuration_receipts", 2)
	fresh, e := f.identity.CreateInvitation(f.ctx, a.Token, f.owner.WorkspaceID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: fresh.Token, Email: "fresh@example.test", Password: recoveryOldPassword, DisplayName: "Fresh"}); e != nil {
		t.Fatal("new tenure invite", e)
	}
}
func TestOwnerTransferRejectedWithoutWrites(t *testing.T) {
	for _, name := range []string{"member", "self", "owner-target", "missing", "suspended-target", "disabled-target", "suspended-actor", "disabled-actor", "expired", "revoked", "future", "old-session", "actor-revision", "target-revision", "overflow", "cross-workspace"} {
		t.Run(name, func(t *testing.T) {
			f := newHandoffFixture(t, 1)
			s := f.session
			b := f.members[0]
			in := handoffInput(b.Account.User.ID, 1)
			want := authz.ErrConflict
			switch name {
			case "member":
				s = b
				in.TargetUserID = f.session.Account.User.ID
				want = authz.ErrForbidden
			case "self":
				in.TargetUserID = s.Account.User.ID
				want = authz.ErrInvalid
			case "owner-target":
				f.exec(t, `UPDATE radishnexus.workspace_memberships SET role='owner',role_revision=role_revision+1 WHERE user_id=$1`, b.Account.User.ID)
			case "missing":
				in.TargetUserID = "usr_missing"
				want = authz.ErrNotFound
			case "suspended-target":
				f.exec(t, `UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE user_id=$1`, b.Account.User.ID)
				want = authz.ErrNotFound
			case "disabled-target":
				f.exec(t, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, b.Account.User.ID)
				want = authz.ErrNotFound
			case "suspended-actor":
				f.exec(t, `UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE user_id=$1`, s.Account.User.ID)
				want = authz.ErrNotFound
			case "disabled-actor":
				f.exec(t, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, s.Account.User.ID)
				want = authn.ErrInvalidSession
			case "expired":
				f.exec(t, `ALTER TABLE radishnexus.user_sessions DISABLE TRIGGER user_sessions_control_mutation; UPDATE radishnexus.user_sessions SET created_at=clock_timestamp()-interval '1 hour',expires_at=clock_timestamp()-interval '1 minute' WHERE token_digest=$1; ALTER TABLE radishnexus.user_sessions ENABLE TRIGGER user_sessions_control_mutation`, tokenHash(s.Token))
				want = authn.ErrInvalidSession
			case "revoked":
				f.exec(t, `UPDATE radishnexus.user_sessions SET revoked_at=clock_timestamp() WHERE token_digest=$1`, tokenHash(s.Token))
				want = authn.ErrInvalidSession
			case "future":
				f.exec(t, `ALTER TABLE radishnexus.user_sessions DISABLE TRIGGER user_sessions_control_mutation; UPDATE radishnexus.user_sessions SET created_at=clock_timestamp()+interval '1 minute' WHERE token_digest=$1; ALTER TABLE radishnexus.user_sessions ENABLE TRIGGER user_sessions_control_mutation`, tokenHash(s.Token))
				want = authn.ErrInvalidSession
			case "old-session":
				f.exec(t, `ALTER TABLE radishnexus.user_sessions DISABLE TRIGGER user_sessions_control_mutation; UPDATE radishnexus.user_sessions SET created_at=clock_timestamp()-interval '10 minutes' WHERE token_digest=$1; ALTER TABLE radishnexus.user_sessions ENABLE TRIGGER user_sessions_control_mutation`, tokenHash(s.Token))
				want = authn.ErrRecentAuthentication
			case "actor-revision":
				in.ExpectedActorRoleRevision = "2"
			case "target-revision":
				in.ExpectedTargetRoleRevision = "2"
			case "overflow":
				f.exec(t, `ALTER TABLE radishnexus.workspace_memberships DISABLE TRIGGER workspace_role_revision; UPDATE radishnexus.workspace_memberships SET role_revision=9223372036854775807 WHERE user_id=$1; ALTER TABLE radishnexus.workspace_memberships ENABLE TRIGGER workspace_role_revision`, s.Account.User.ID)
				in.ExpectedActorRoleRevision = "9223372036854775807"
			case "cross-workspace":
				f.exec(t, `DELETE FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2`, f.owner.WorkspaceID, b.Account.User.ID)
				want = authz.ErrNotFound
			}
			before := f.snapshot(t)
			if _, e := f.apply(f.ctx, s, in); !errors.Is(e, want) {
				t.Fatalf("got %v want %v", e, want)
			}
			if !reflect.DeepEqual(before, f.snapshot(t)) {
				t.Fatal("rejection wrote data")
			}
		})
	}
}
func TestOwnerTransferRollbackAtEveryWrite(t *testing.T) {
	for _, point := range []string{"promote", "demote", "audit", "receipt", "deferred"} {
		t.Run(point, func(t *testing.T) {
			f := newHandoffFixture(t, 1)
			table, event, condition := "workspace_memberships", "UPDATE", "NEW.role='owner'"
			prefix := "CREATE TRIGGER handoff_fail AFTER "
			switch point {
			case "demote":
				condition = "NEW.role='member'"
			case "audit":
				table = "workspace_configuration_audit"
				event = "INSERT"
				condition = "true"
			case "receipt":
				table = "workspace_configuration_receipts"
				event = "INSERT"
				condition = "true"
			case "deferred":
				table = "workspace_configuration_receipts"
				event = "INSERT"
				condition = "true"
				prefix = "CREATE CONSTRAINT TRIGGER handoff_fail AFTER "
			}
			def := ""
			if point == "deferred" {
				def = " DEFERRABLE INITIALLY DEFERRED"
			}
			f.exec(t, `CREATE FUNCTION radishnexus.handoff_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF `+condition+` THEN RAISE EXCEPTION 'injected handoff failure'; END IF; RETURN NEW; END $$; `+prefix+event+` ON radishnexus.`+table+def+` FOR EACH ROW EXECUTE FUNCTION radishnexus.handoff_fail()`)
			before := f.snapshot(t)
			in := handoffInput(f.members[0].Account.User.ID, 1)
			if _, e := f.apply(f.ctx, f.session, in); e == nil || errors.Is(e, goldenpath.ErrOwnerTransferUnconfirmed) {
				t.Fatal("known rollback classified wrong", e)
			}
			if !reflect.DeepEqual(before, f.snapshot(t)) {
				t.Fatal("partial transfer")
			}
			f.exec(t, `DROP TRIGGER handoff_fail ON radishnexus.`+table)
			if _, e := f.apply(f.ctx, f.session, in); e != nil {
				t.Fatal("rollback prevented original retry", e)
			}
		})
	}
}
func TestOwnerTransferConcurrentOperations(t *testing.T) {
	for _, mode := range []string{"same-request", "same-owner-two-targets", "same-id-two-targets", "same-id-two-versions", "two-owners-one-target", "two-owners-two-targets"} {
		t.Run(mode, func(t *testing.T) {
			f := newHandoffFixture(t, 3)
			a, b, c, d := f.session, f.members[0], f.members[1], f.members[2]
			s2 := a
			i1 := handoffInput(b.Account.User.ID, 1)
			i2 := i1
			want := 1
			switch mode {
			case "same-owner-two-targets":
				i2 = handoffInput(c.Account.User.ID, 2)
			case "same-id-two-versions":
				i2.ExpectedTargetRoleRevision = "2"
			case "same-id-two-targets":
				i2.TargetUserID = c.Account.User.ID
			case "two-owners-one-target", "two-owners-two-targets":
				f.exec(t, `UPDATE radishnexus.workspace_memberships SET role='owner',role_revision=role_revision+1 WHERE user_id=$1`, c.Account.User.ID)
				s2 = c
				i2 = handoffInput(b.Account.User.ID, 2)
				i2.ExpectedActorRoleRevision = "2"
				if mode == "two-owners-two-targets" {
					i2.TargetUserID = d.Account.User.ID
					want = 2
				}
			}
			gate := f.gate(t, `SELECT id FROM radishnexus.workspaces WHERE id=$1 FOR UPDATE`, f.owner.WorkspaceID)
			type outcome struct {
				r goldenpath.OwnerTransferResult
				e error
			}
			results := make(chan outcome, 2)
			go func() { r, e := f.apply(f.ctx, a, i1); results <- outcome{r, e} }()
			f.wait(t, "%FROM radishnexus.workspaces%")
			go func() { r, e := f.apply(f.ctx, s2, i2); results <- outcome{r, e} }()
			if mode != "two-owners-two-targets" {
				f.wait(t, "%FROM radishnexus.workspace_memberships%")
			}
			if e := gate.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			success := 0
			for range 2 {
				o := <-results
				if o.e == nil {
					success++
				} else if !errors.Is(o.e, authz.ErrForbidden) && !errors.Is(o.e, authz.ErrConflict) {
					t.Fatal(o.e)
				}
			}
			expectedSuccess := want
			if mode == "same-request" {
				expectedSuccess = 2
			}
			if success != expectedSuccess {
				t.Fatal(success, expectedSuccess)
			}
			f.count(t, "workspace_configuration_audit", want)
			f.count(t, "workspace_configuration_receipts", want)
			var owners int
			if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND status='active' AND role='owner'`, f.owner.WorkspaceID).Scan(&owners); e != nil {
				t.Fatal(e)
			}
			expectedOwners := 1
			if strings.HasPrefix(mode, "two-owners") {
				expectedOwners = 2
			}
			if owners != expectedOwners {
				t.Fatal("owner count changed", owners)
			}
		})
	}
}

func TestOwnerTransferLostCommitAndInFlightStatus(t *testing.T) {
	f := newHandoffFixture(t, 1)
	in := handoffInput(f.members[0].Account.User.ID, 1)
	pool, finish := lostCommitPool(t, f.ctx, f.pool)
	service := goldenpath.NewOwnerTransferService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{})
	// Blocking on the target account precedes actor membership locking, so a
	// status read can truthfully observe no committed receipt while POST waits.
	gate := f.gate(t, `SELECT user_id FROM radishnexus.user_accounts WHERE user_id=$1 FOR UPDATE`, in.TargetUserID)
	done := make(chan error, 1)
	go func() {
		_, e := service.Transfer(f.ctx, goldenpath.Invocation{Principal: f.principal(f.session)}, f.session.Token, in)
		done <- e
	}()
	f.wait(t, "%FROM radishnexus.user_accounts%")
	status, e := f.transfer.Status(f.ctx, f.principal(f.session), f.session.Token, in.ClientOperationID)
	if e != nil || status.Result != "not_observed" {
		t.Fatal(e, status)
	}
	if e = gate.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	e = <-done
	finish()
	if !errors.Is(e, goldenpath.ErrOwnerTransferUnconfirmed) {
		t.Fatal("lost actual COMMIT not uncertain", e)
	}
	status, e = f.transfer.Status(f.ctx, f.principal(f.session), f.session.Token, in.ClientOperationID)
	if e != nil || status.Result != "applied" {
		t.Fatal(e, status)
	}
	retry, e := f.apply(f.ctx, f.session, in)
	if e != nil || retry.AuditID != status.AuditID {
		t.Fatal(e, retry)
	}
	f.count(t, "workspace_configuration_audit", 1)
	f.count(t, "workspace_configuration_receipts", 1)
}
func TestOwnerTransferLockTimeAndCancellation(t *testing.T) {
	for _, mode := range []string{"recent-boundary", "expired-after-wait", "timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			f := newHandoffFixture(t, 1)
			if mode == "recent-boundary" {
				f.exec(t, `ALTER TABLE radishnexus.user_sessions DISABLE TRIGGER user_sessions_control_mutation; UPDATE radishnexus.user_sessions SET created_at=clock_timestamp()-interval '599 seconds' WHERE token_digest=$1; ALTER TABLE radishnexus.user_sessions ENABLE TRIGGER user_sessions_control_mutation`, tokenHash(f.session.Token))
			}
			if mode == "expired-after-wait" {
				f.exec(t, `ALTER TABLE radishnexus.user_sessions DISABLE TRIGGER user_sessions_control_mutation; UPDATE radishnexus.user_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE token_digest=$1; ALTER TABLE radishnexus.user_sessions ENABLE TRIGGER user_sessions_control_mutation`, tokenHash(f.session.Token))
			}
			gate := f.gate(t, `SELECT id FROM radishnexus.workspaces WHERE id=$1 FOR UPDATE`, f.owner.WorkspaceID)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			before := f.snapshot(t)
			done := make(chan error, 1)
			go func() { _, e := f.apply(ctx, f.session, handoffInput(f.members[0].Account.User.ID, 1)); done <- e }()
			f.wait(t, "%FROM radishnexus.workspaces%")
			if mode == "cancel" {
				cancel()
			} else if mode != "timeout" {
				time.Sleep(1100 * time.Millisecond)
				if e := gate.Commit(f.ctx); e != nil {
					t.Fatal(e)
				}
			}
			e := <-done
			if e == nil || errors.Is(e, goldenpath.ErrOwnerTransferUnconfirmed) {
				t.Fatal("wait failure classification", e)
			}
			if mode == "recent-boundary" && !errors.Is(e, authn.ErrRecentAuthentication) {
				t.Fatal(e)
			}
			if mode == "expired-after-wait" && !errors.Is(e, authn.ErrInvalidSession) {
				t.Fatal(e)
			}
			if mode == "timeout" {
				var state interface{ SQLState() string }
				if !errors.As(e, &state) || state.SQLState() != "55P03" {
					t.Fatal("expected lock timeout", e)
				}
			}
			_ = gate.Rollback(f.ctx)
			if !reflect.DeepEqual(before, f.snapshot(t)) {
				t.Fatal("wait failure wrote data")
			}
		})
	}
}
func TestOwnerTransferTargetStateSerializes(t *testing.T) {
	for _, mode := range []string{"disable-first", "suspend-first", "transfer-first-disable", "transfer-first-suspend"} {
		t.Run(mode, func(t *testing.T) {
			f := newHandoffFixture(t, 1)
			target := f.members[0].Account.User.ID
			update := `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`
			pattern := "%FROM radishnexus.user_accounts%"
			if strings.Contains(mode, "suspend") {
				update = `UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE user_id=$1`
				pattern = "%FROM radishnexus.workspace_memberships%"
			}
			done := make(chan error, 1)
			if !strings.HasPrefix(mode, "transfer-first") {
				gate := f.gate(t, update, target)
				go func() { _, e := f.apply(f.ctx, f.session, handoffInput(target, 1)); done <- e }()
				f.wait(t, pattern)
				if e := gate.Commit(f.ctx); e != nil {
					t.Fatal(e)
				}
				if e := <-done; !errors.Is(e, authz.ErrNotFound) {
					t.Fatal(e)
				}
				f.count(t, "workspace_configuration_audit", 0)
			} else {
				gate := f.gate(t, `SELECT id FROM radishnexus.workspaces WHERE id=$1 FOR UPDATE`, f.owner.WorkspaceID)
				go func() { _, e := f.apply(f.ctx, f.session, handoffInput(target, 1)); done <- e }()
				f.wait(t, "%FROM radishnexus.workspaces%")
				changed := make(chan error, 1)
				go func() { _, e := f.pool.Exec(f.ctx, update, target); changed <- e }()
				f.wait(t, "UPDATE radishnexus.%")
				if e := gate.Commit(f.ctx); e != nil {
					t.Fatal(e)
				}
				if e := <-done; e != nil {
					t.Fatal(e)
				}
				if e := <-changed; e != nil {
					t.Fatal(e)
				}
				f.count(t, "workspace_configuration_audit", 1)
			}
		})
	}
}
func TestOwnerTransferCandidatePaginationAndDBGuards(t *testing.T) {
	f := newHandoffFixture(t, 27)
	p := f.principal(f.session)
	first, e := f.transfer.Candidates(f.ctx, p, f.session.Token, "", 25)
	if e != nil || len(first.Items) != 25 || first.NextID == "" {
		t.Fatal(e, len(first.Items))
	}
	second, e := f.transfer.Candidates(f.ctx, p, f.session.Token, first.NextID, 25)
	if e != nil || len(second.Items) != 2 || second.NextID != "" || second.Items[0].ID <= first.NextID {
		t.Fatal(e, second)
	}
	for _, query := range []string{`UPDATE radishnexus.workspace_memberships SET role='member' WHERE role='owner'`, `UPDATE radishnexus.workspace_memberships SET role_revision=role_revision+1`, `UPDATE radishnexus.workspace_memberships SET role_revision=0`} {
		if _, e = f.pool.Exec(f.ctx, query); e == nil {
			t.Fatal("invalid revision accepted")
		}
	}
	in := handoffInput(f.members[0].Account.User.ID, 1)
	out, e := f.apply(f.ctx, f.session, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.transfer.Candidates(f.ctx, p, f.session.Token, first.NextID, 25); !errors.Is(e, authz.ErrForbidden) {
		t.Fatal("cursor granted authority", e)
	}
	for _, table := range []string{"workspace_configuration_audit", "workspace_configuration_receipts"} {
		for _, verb := range []string{"DELETE FROM ", "UPDATE "} {
			sql := verb + "radishnexus." + table
			if verb == "UPDATE " {
				sql += " SET actor_id=actor_id"
			}
			if _, e = f.pool.Exec(f.ctx, sql); e == nil {
				t.Fatal("history mutable", table, verb)
			}
		}
	}
	var shape bool
	if e = f.pool.QueryRow(f.ctx, `SELECT actor_before_role='owner' AND actor_after_role='member' AND before_state='member' AND after_state='owner' AND actor_before_role_revision=1 AND actor_after_role_revision=2 AND subject_before_role_revision=1 AND subject_after_role_revision=2 AND changed FROM radishnexus.workspace_configuration_audit WHERE id=$1`, out.AuditID).Scan(&shape); e != nil || !shape {
		t.Fatal("audit shape", e)
	}
	for _, alteration := range []string{`'actor_id',a.subject_id`, `'subject_id',a.actor_id`, `'actor_before_role','member'`, `'actor_after_role',NULL`, `'actor_before_role_revision',0`, `'actor_after_role_revision',9`, `'subject_after_role_revision',NULL`, `'authorization_id','dpa_forbidden'`, `'granted_user_ids',jsonb_build_array(a.actor_id)`} {
		sql := `INSERT INTO radishnexus.workspace_configuration_audit SELECT (jsonb_populate_record(NULL::radishnexus.workspace_configuration_audit,to_jsonb(a)||jsonb_build_object('id','cfa_invalid',` + alteration + `))).* FROM radishnexus.workspace_configuration_audit a WHERE id=$1`
		if _, e = f.pool.Exec(f.ctx, sql, out.AuditID); e == nil {
			t.Fatal("invalid audit accepted", alteration)
		}
	}
	// Bypass application deduplication to prove the DB key excludes subject.
	otherTarget := f.members[1].Account.User.ID
	if _, e = f.pool.Exec(f.ctx, `INSERT INTO radishnexus.workspace_configuration_audit SELECT (jsonb_populate_record(NULL::radishnexus.workspace_configuration_audit,to_jsonb(a)||jsonb_build_object('id','cfa_duplicate_operation','subject_id',$2::text,'result_id',$2::text))).* FROM radishnexus.workspace_configuration_audit a WHERE id=$1`, out.AuditID, otherTarget); e != nil {
		t.Fatal(e)
	}
	_, e = f.pool.Exec(f.ctx, `INSERT INTO radishnexus.workspace_configuration_receipts SELECT (jsonb_populate_record(NULL::radishnexus.workspace_configuration_receipts,to_jsonb(r)||jsonb_build_object('audit_id','cfa_duplicate_operation','subject_id',$2::text))).* FROM radishnexus.workspace_configuration_receipts r WHERE audit_id=$1`, out.AuditID, otherTarget)
	var pgerr *pgconn.PgError
	if !errors.As(e, &pgerr) || pgerr.Code != "23505" || pgerr.ConstraintName != "owner_transfer_operation" {
		t.Fatal("subject-independent operation constraint", e)
	}
}

func TestOwnerTransferInvitationAndOIDCTenure(t *testing.T) {
	f := newHandoffFixture(t, 2)
	a, b, c := f.session, f.members[0], f.members[1]
	// Explicit multi-owner/other-workspace fixture. It must not invalidate those invites.
	f.exec(t, `UPDATE radishnexus.workspace_memberships SET role='owner',role_revision=role_revision+1 WHERE user_id=$1; INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_invite_other','Other'); INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,status,role) VALUES('wrk_invite_other',$2,'active','owner')`, c.Account.User.ID, a.Account.User.ID)
	old, e := f.identity.CreateInvitation(f.ctx, a.Token, f.owner.WorkspaceID)
	if e != nil {
		t.Fatal(e)
	}
	other, e := f.identity.CreateInvitation(f.ctx, a.Token, "wrk_invite_other")
	if e != nil {
		t.Fatal(e)
	}
	coowner, e := f.identity.CreateInvitation(f.ctx, c.Token, f.owner.WorkspaceID)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	transaction := authn.OIDCTransaction{StateDigest: tokenHash("state"), BrowserDigest: tokenHash("browser"), NonceDigest: tokenHash("nonce"), ProviderDigest: tokenHash("provider"), InvitationDigest: tokenHash(old.Token), DisplayName: "Federated", CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
	if e = f.store.BeginOIDC(f.ctx, transaction); e != nil {
		t.Fatal(e)
	}
	// Existing bound identity remains a login identity across Workspace changes.
	binding := authn.OIDCCompletion{Identity: authn.ExternalIdentity{Issuer: "https://id.example.test", Subject: "handoff-subject"}, Transaction: authn.OIDCTransaction{InitiatingSessionDigest: tokenHash(a.Token), ExpiresAt: now.Add(time.Minute)}, Session: authn.SessionRecord{CreatedAt: now}, AuditID: "ida_handoff_binding"}
	if e = f.store.CompleteOIDC(f.ctx, binding); e != nil {
		t.Fatal(e)
	}
	if _, e = f.apply(f.ctx, a, handoffInput(b.Account.User.ID, 1)); e != nil {
		t.Fatal(e)
	}
	completion := authn.OIDCCompletion{Transaction: transaction, Identity: authn.ExternalIdentity{Issuer: "https://id.example.test", Subject: "new-subject"}, NewUserID: "usr_federated", Session: authn.SessionRecord{ID: "ses_federated", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(authn.SessionLifetime), TokenDigest: tokenHash("federated token"), CSRFTokenDigest: tokenHash("federated csrf")}, AuditID: "ida_federated"}
	before := f.snapshot(t)
	if e = f.store.CompleteOIDC(f.ctx, completion); !errors.Is(e, authn.ErrInvitationInvalid) {
		t.Fatal("old start admitted at callback", e)
	}
	if !reflect.DeepEqual(before, f.snapshot(t)) {
		t.Fatal("failed callback wrote")
	}
	if e = f.store.BeginOIDC(f.ctx, transaction); !errors.Is(e, authn.ErrInvitationInvalid) {
		t.Fatal("old invite start accepted", e)
	}
	completion.Transaction.InvitationDigest = nil
	completion.Identity = binding.Identity
	if e = f.store.CompleteOIDC(f.ctx, completion); e != nil {
		t.Fatal("bound login failed", e)
	}
	fresh, e := f.identity.CreateInvitation(f.ctx, b.Token, f.owner.WorkspaceID)
	if e != nil {
		t.Fatal(e)
	}
	for n, invite := range []authn.Invitation{other, coowner, fresh} {
		if _, e = f.identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: invite.Token, Email: fmt.Sprintf("tenure-%d@example.test", n), Password: recoveryOldPassword, DisplayName: "Joined"}); e != nil {
			t.Fatal("unrelated or new invitation invalidated", e)
		}
	}
	if role, revision := f.state(t, c); role != "owner" || revision != 2 {
		t.Fatal("other owner changed", role, revision)
	}
}

func TestOwnerTransferInvitationInterleavings(t *testing.T) {
	for _, mode := range []string{"create-first", "transfer-first-create", "accept-first", "transfer-first-accept", "existing-recipient"} {
		t.Run(mode, func(t *testing.T) {
			f := newHandoffFixture(t, 1)
			a, b := f.session, f.members[0]
			invite, e := f.identity.CreateInvitation(f.ctx, a.Token, f.owner.WorkspaceID)
			if e != nil {
				t.Fatal(e)
			}
			accept := func() error {
				input := authn.AcceptInvitationInput{InvitationToken: invite.Token, Email: "join-race@example.test", Password: recoveryOldPassword, DisplayName: "Racer"}
				if mode == "existing-recipient" {
					input = authn.AcceptInvitationInput{InvitationToken: invite.Token, SessionToken: b.Token}
				}
				_, e := f.identity.AcceptInvitation(f.ctx, input)
				return e
			}
			action := func() error {
				if strings.Contains(mode, "create") {
					_, e := f.identity.CreateInvitation(f.ctx, a.Token, f.owner.WorkspaceID)
					return e
				}
				return accept()
			}
			transferDone, actionDone := make(chan error, 1), make(chan error, 1)
			transfer := func() { _, e := f.apply(f.ctx, a, handoffInput(b.Account.User.ID, 1)); transferDone <- e }
			if strings.HasPrefix(mode, "transfer-first") {
				gate := f.gate(t, `SELECT id FROM radishnexus.workspaces WHERE id=$1 FOR UPDATE`, f.owner.WorkspaceID)
				go transfer()
				f.wait(t, "%FROM radishnexus.workspaces%")
				go func() { actionDone <- action() }()
				pattern := "%FROM radishnexus.workspace_memberships%"
				if mode == "transfer-first-create" {
					pattern = "%FROM radishnexus.user_accounts%"
				}
				f.wait(t, pattern)
				if e = gate.Commit(f.ctx); e != nil {
					t.Fatal(e)
				}
				if e = <-transferDone; e != nil {
					t.Fatal(e)
				}
				e = <-actionDone
				want := authn.ErrInvitationInvalid
				if mode == "transfer-first-create" {
					want = authz.ErrForbidden
				}
				if !errors.Is(e, want) {
					t.Fatal(e)
				}
			} else {
				// A test-only AFTER audit barrier holds the real admission locks until released.
				f.exec(t, `CREATE FUNCTION radishnexus.handoff_invite_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(42,3); RETURN NEW; END $$; CREATE TRIGGER handoff_invite_barrier AFTER INSERT ON radishnexus.identity_audit FOR EACH ROW EXECUTE FUNCTION radishnexus.handoff_invite_barrier()`)
				gate := f.gate(t, `SELECT pg_advisory_xact_lock(42,3)`)
				go func() { actionDone <- action() }()
				f.wait(t, "%INSERT INTO radishnexus.identity_audit%")
				go transfer()
				pattern := "%FROM radishnexus.workspace_memberships%"
				if mode == "create-first" || mode == "existing-recipient" {
					pattern = "%FROM radishnexus.user_accounts%"
				}
				f.wait(t, pattern)
				if e = gate.Commit(f.ctx); e != nil {
					t.Fatal(e)
				}
				if e = <-actionDone; e != nil {
					t.Fatal(e)
				}
				if e = <-transferDone; e != nil {
					t.Fatal(e)
				}
			}
			f.count(t, "workspace_configuration_audit", 1)
			if role, _ := f.state(t, b); role != "owner" {
				t.Fatal("recipient role overwritten")
			}
		})
	}
}

func TestOwnerTransferRevocationInterleavings(t *testing.T) {
	for _, kind := range []string{"recovery", "unlink", "logout"} {
		for _, first := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s-transfer-first-%v", kind, first), func(t *testing.T) {
				f := newHandoffFixture(t, 1)
				if kind == "unlink" {
					now := time.Now()
					if e := f.store.CompleteOIDC(f.ctx, authn.OIDCCompletion{Identity: authn.ExternalIdentity{Issuer: "https://id.example.test", Subject: "unlink-owner"}, Transaction: authn.OIDCTransaction{InitiatingSessionDigest: tokenHash(f.session.Token), ExpiresAt: now.Add(time.Minute)}, Session: authn.SessionRecord{CreatedAt: now}, AuditID: "ida_link_before_transfer"}); e != nil {
						t.Fatal(e)
					}
				}
				recovery := f.input(t, 10)
				revoke := func() error {
					switch kind {
					case "recovery":
						_, e := f.service.Apply(f.ctx, recovery)
						return e
					case "unlink":
						return f.store.UnlinkExternal(f.ctx, tokenHash(f.session.Token), "https://id.example.test", "ida_unlink_transfer", time.Now())
					default:
						return f.auth.RevokeSession(f.ctx, f.session.Token, f.session.CSRFToken)
					}
				}
				in := handoffInput(f.members[0].Account.User.ID, 1)
				if first {
					gate := f.gate(t, `SELECT id FROM radishnexus.workspaces WHERE id=$1 FOR UPDATE`, f.owner.WorkspaceID)
					transferDone, revokeDone := make(chan error, 1), make(chan error, 1)
					go func() { _, e := f.apply(f.ctx, f.session, in); transferDone <- e }()
					f.wait(t, "%FROM radishnexus.workspaces%")
					go func() { revokeDone <- revoke() }()
					pattern := "%FROM radishnexus.user_accounts%"
					if kind == "logout" {
						pattern = "%UPDATE radishnexus.user_sessions%"
					}
					f.wait(t, pattern)
					if e := gate.Commit(f.ctx); e != nil {
						t.Fatal(e)
					}
					if e := <-transferDone; e != nil {
						t.Fatal(e)
					}
					if e := <-revokeDone; e != nil {
						t.Fatal(e)
					}
					f.count(t, "workspace_configuration_audit", 1)
				} else {
					// Finish the real identity command before attempting its now-revoked Session.
					if e := revoke(); e != nil {
						t.Fatal(e)
					}
					before := f.snapshot(t)
					if _, e := f.apply(f.ctx, f.session, in); !errors.Is(e, authn.ErrInvalidSession) {
						t.Fatal(e)
					}
					if !reflect.DeepEqual(before, f.snapshot(t)) {
						t.Fatal("revoked request wrote")
					}
				}
				if _, e := f.transfer.Status(f.ctx, f.principal(f.session), f.session.Token, in.ClientOperationID); !errors.Is(e, authn.ErrInvalidSession) {
					t.Fatal("receipt bypassed revoked session", e)
				}
			})
		}
	}
}
func TestOwnerTransferRealDeadlockAndCancelAfterWrites(t *testing.T) {
	for _, deadlock := range []bool{true, false} {
		t.Run(fmt.Sprintf("deadlock-%v", deadlock), func(t *testing.T) {
			f := newHandoffFixture(t, 1)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			f.exec(t, `CREATE FUNCTION radishnexus.handoff_write_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(42,7); RETURN NEW; END $$; CREATE TRIGGER handoff_write_barrier AFTER INSERT ON radishnexus.workspace_configuration_audit FOR EACH ROW EXECUTE FUNCTION radishnexus.handoff_write_barrier()`)
			gate := f.gate(t, `SELECT pg_advisory_xact_lock(42,7)`)
			// The short detector belongs only to this disposable test connection. The
			// artificial inverse lock is deliberately outside production lock order.
			config := f.pool.Config().Copy()
			config.ConnConfig.RuntimeParams["deadlock_timeout"] = "100ms"
			pool, e := pgxpool.NewWithConfig(f.ctx, config)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			service := goldenpath.NewOwnerTransferService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{})
			before := f.snapshot(t)
			done := make(chan error, 1)
			go func() {
				_, e := service.Transfer(ctx, goldenpath.Invocation{Principal: f.principal(f.session)}, f.session.Token, handoffInput(f.members[0].Account.User.ID, 1))
				done <- e
			}()
			f.wait(t, "%INSERT INTO radishnexus.workspace_configuration_audit%")
			if deadlock {
				if _, e = gate.Exec(f.ctx, `SELECT user_id FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2 FOR UPDATE`, f.owner.WorkspaceID, f.owner.UserID); e != nil {
					t.Fatal("unexpected victim", e)
				}
			} else {
				cancel()
			}
			_ = gate.Rollback(f.ctx)
			e = <-done
			if e == nil || errors.Is(e, goldenpath.ErrOwnerTransferUnconfirmed) {
				t.Fatal("known rollback classified uncertain", e)
			}
			if deadlock {
				var state interface{ SQLState() string }
				if !errors.As(e, &state) || state.SQLState() != "40P01" {
					t.Fatal("not a real PostgreSQL deadlock", e)
				}
			}
			if !reflect.DeepEqual(before, f.snapshot(t)) {
				t.Fatal("partial transfer after deadlock/cancel")
			}
		})
	}
}

func TestOwnerTransferConfigurationLockOrder(t *testing.T) {
	for _, kind := range []string{"team.create", "project.create", "component.create", "environment.create", "repository.create", "environment.authorization.grant", "component.repository.link"} {
		for _, transferFirst := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s-transfer-first-%v", kind, transferFirst), func(t *testing.T) {
				f := newHandoffFixture(t, 1)
				service := goldenpath.NewConfigurationService(goldenpostgres.New(f.pool), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
				call := func(input goldenpath.ConfigurationInput) (goldenpath.ConfigurationResult, error) {
					return service.Configure(f.ctx, goldenpath.Invocation{Principal: f.principal(f.session), SourceKind: "web", CorrelationID: "owner-order"}, input)
				}
				must := func(input goldenpath.ConfigurationInput) goldenpath.ConfigurationResult {
					t.Helper()
					r, e := call(input)
					if e != nil {
						t.Fatal(e)
					}
					return r
				}
				team := must(goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: f.owner.WorkspaceID, ClientOperationID: "base-team", Name: "Team"}).Object.ID
				component := must(goldenpath.ConfigurationInput{Kind: "component.create", ScopeID: f.owner.WorkspaceID, ClientOperationID: "base-component", Name: "Service", Key: "service", OwnerTeamID: team, Delivery: &goldenpath.DeliveryConfigurationInput{Type: "service"}}).Object.ID
				environment := must(goldenpath.ConfigurationInput{Kind: "environment.create", ScopeID: f.owner.WorkspaceID, ClientOperationID: "base-environment", Name: "Staging", Key: "staging", OwnerTeamID: team, Delivery: &goldenpath.DeliveryConfigurationInput{Classification: "staging"}}).Object.ID
				repository := goldenpath.RepositoryConfigurationInput{RepositoryMetadata: goldenpath.RepositoryMetadata{Provider: "gitea", ProviderOrigin: "https://git.example.test", ExternalID: "123", WebURL: "https://git.example.test/team/service", DefaultBranch: "main"}}
				repo := must(goldenpath.ConfigurationInput{Kind: "repository.create", ScopeID: f.owner.WorkspaceID, ClientOperationID: "base-repository", Name: "Source", Repository: &repository}).Object.ID
				input := goldenpath.ConfigurationInput{Kind: kind, ScopeID: f.owner.WorkspaceID, ClientOperationID: "concurrent-config", Name: "Next", Key: "next", OwnerTeamID: team}
				switch kind {
				case "team.create":
					input.Key = ""
					input.OwnerTeamID = ""
				case "project.create":
					input.InitialAdminUserID = f.session.Account.User.ID
					input.Visibility = "restricted"
				case "component.create":
					input.Delivery = &goldenpath.DeliveryConfigurationInput{Type: "service"}
				case "environment.create":
					input.Delivery = &goldenpath.DeliveryConfigurationInput{Classification: "staging"}
				case "repository.create":
					input.Key = ""
					input.OwnerTeamID = ""
					repository.ExternalID = "456"
					repository.WebURL = "https://git.example.test/team/next"
					input.Repository = &repository
				case "environment.authorization.grant":
					input.Name = ""
					input.Key = ""
					input.OwnerTeamID = ""
					input.ScopeID = environment
					input.UserID = f.members[0].Account.User.ID
					input.Delivery = &goldenpath.DeliveryConfigurationInput{Confirmed: true}
				case "component.repository.link":
					input.Name = ""
					input.Key = ""
					input.OwnerTeamID = ""
					input.ScopeID = component
					input.Repository = &goldenpath.RepositoryConfigurationInput{RepositoryID: repo, Confirmed: true}
				}
				f.exec(t, `CREATE FUNCTION radishnexus.owner_config_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(42,5); RETURN NEW; END $$; CREATE TRIGGER owner_config_barrier BEFORE INSERT ON radishnexus.workspace_configuration_audit FOR EACH ROW EXECUTE FUNCTION radishnexus.owner_config_barrier()`)
				gate := f.gate(t, `SELECT pg_advisory_xact_lock(42,5)`)
				configDone, transferDone := make(chan error, 1), make(chan error, 1)
				configCall := func() { _, e := call(input); configDone <- e }
				transferCall := func() {
					_, e := f.apply(f.ctx, f.session, handoffInput(f.members[0].Account.User.ID, 1))
					transferDone <- e
				}
				if transferFirst {
					go transferCall()
				} else {
					go configCall()
				}
				f.wait(t, "%INSERT INTO radishnexus.workspace_configuration_audit%")
				if transferFirst {
					go configCall()
				} else {
					go transferCall()
				}
				f.wait(t, "%FROM radishnexus.workspace_memberships%")
				if e := gate.Commit(f.ctx); e != nil {
					t.Fatal(e)
				}
				if e := <-transferDone; e != nil {
					t.Fatal(e)
				}
				e := <-configDone
				if transferFirst && !errors.Is(e, authz.ErrForbidden) {
					t.Fatal("post-transfer command admitted", e)
				}
				if !transferFirst && e != nil {
					t.Fatal("prior authorized command failed", e)
				}
				// Existing receipts still require owner after the completed transfer.
				if _, e = call(input); !errors.Is(e, authz.ErrForbidden) {
					t.Fatal("configuration receipt bypass", e)
				}
			})
		}
	}
}

func TestOwnerTransferHTTPWithCurrentSessionAuthority(t *testing.T) {
	f := newHandoffFixture(t, 2)
	a, b, c := f.session, f.members[0], f.members[1]
	policy, e := httptransport.NewBrowserSessionPolicy("https://nexus.example.test")
	if e != nil {
		t.Fatal(e)
	}
	proxy, e := httptransport.NewTrustedProxyPolicy("10.0.0.0/8")
	if e != nil {
		t.Fatal(e)
	}
	handler := httptransport.WithRequestID(httptransport.NewOwnerTransferHandler(f.auth, f.transfer, policy, proxy))
	request := func(s authn.Session, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest(method, "https://nexus.example.test"+path, bytes.NewReader(raw))
		r.AddCookie(&http.Cookie{Name: httptransport.SessionCookieName, Value: s.Token})
		r.AddCookie(&http.Cookie{Name: httptransport.CSRFCookieName, Value: s.CSRFToken})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://nexus.example.test")
		r.Header.Set(httptransport.CSRFHeaderName, s.CSRFToken)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		return out
	}
	base := "/api/v1/workspaces/" + f.owner.WorkspaceID
	in := handoffInput(b.Account.User.ID, 1)
	if r := request(b, "POST", base+"/owner-transfers", handoffInput(c.Account.User.ID, 2)); r.Code != 403 {
		t.Fatal(r.Code, r.Body.String())
	}
	out := request(a, "POST", base+"/owner-transfers", in)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	for _, r := range []*httptest.ResponseRecorder{request(a, "POST", base+"/owner-transfers", in), request(a, "GET", base+"/owner-transfers/"+in.ClientOperationID, nil)} {
		if r.Code != 200 || r.Body.String() != out.Body.String() {
			t.Fatal("historical HTTP changed", r.Code, r.Body.String())
		}
	}
	if r := request(a, "GET", base+"/owner-transfer-candidates", nil); r.Code != 403 {
		t.Fatal(r.Code)
	}
	if r := request(c, "GET", base+"/owner-transfers/"+in.ClientOperationID, nil); r.Code != 200 || !strings.Contains(r.Body.String(), "not_observed") || strings.Contains(r.Body.String(), a.Account.User.ID) {
		t.Fatal("other actor read receipt", r.Code)
	}
	f.exec(t, `INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_http_other','Other'); INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,status,role) VALUES('wrk_http_other',$1,'active','member')`, a.Account.User.ID)
	if r := request(a, "GET", "/api/v1/workspaces/wrk_http_other/owner-transfers/"+in.ClientOperationID, nil); r.Code != 200 || !strings.Contains(r.Body.String(), "not_observed") {
		t.Fatal("cross-scope history", r.Code)
	}
	if r := request(b, "GET", "/api/v1/workspaces/wrk_http_other/ownership", nil); r.Code != 404 {
		t.Fatal("nonmember scope disclosed", r.Code)
	}
	f.exec(t, `UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE workspace_id=$1 AND user_id=$2`, f.owner.WorkspaceID, a.Account.User.ID)
	for _, r := range []*httptest.ResponseRecorder{request(a, "GET", base+"/owner-transfers/"+in.ClientOperationID, nil), request(a, "POST", base+"/owner-transfers", in)} {
		if r.Code != 404 {
			t.Fatal("suspended history bypass", r.Code)
		}
	}
}
