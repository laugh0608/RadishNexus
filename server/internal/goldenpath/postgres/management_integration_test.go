//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"github.com/laugh0608/RadishNexus/server/internal/testsupport/pgfault"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

type membershipFixture struct {
	*contextFixture
	management *goldenpath.ManagementService
	members    []authn.Session
	operation  int
}

func newMembershipFixture(t *testing.T) *membershipFixture {
	t.Helper()
	f := &membershipFixture{contextFixture: contextDatabase(t)}
	f.management = goldenpath.NewManagementService(f.store, goldenpath.CryptoIDGenerator{})
	for i := 0; i < 3; i++ {
		s, _ := f.invite(t, fmt.Sprintf("membership-%d@example.test", i))
		f.members = append(f.members, s)
	}
	team := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: f.workspace, Name: "Membership team"})
	project := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: f.workspace, Name: "Private project", Key: "membership", OwnerTeamID: team.Object.ID, Visibility: "restricted", InitialAdminUserID: f.principal.ID})
	f.project = project.Object.ID
	for _, s := range f.members {
		f.mustChange(t, f.owner, goldenpath.ManagementProposal{ScopeKind: "project", ScopeID: f.project, Action: "add", TargetUserID: s.Account.User.ID, Role: "contributor"})
	}
	channel := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: f.project, Name: "Private channel", Visibility: "restricted", MemberUserIDs: []string{f.principal.ID}})
	f.channel = channel.Object.ID
	message, e := f.core.CreateMessage(f.ctx, contextInvocation(f.principal), goldenpath.CreateMessageInput{ChannelID: f.channel, ClientOperationID: "membership-message", Body: "Synthetic retained evidence"})
	if e != nil {
		t.Fatal(e)
	}
	f.message = message.Message.ID
	thread, e := f.core.StartThreadFromMessage(f.ctx, contextInvocation(f.principal), goldenpath.StartThreadFromMessageInput{ChannelID: f.channel, MessageID: f.message, Title: "Private responsibility", Visibility: "restricted"})
	if e != nil {
		t.Fatal(e)
	}
	f.thread = thread.ID
	return f
}
func (f *membershipFixture) principalFor(s authn.Session) authz.Principal {
	return authz.Principal{Kind: authz.PrincipalUser, ID: s.Account.User.ID, WorkspaceID: f.workspace}
}
func (f *membershipFixture) prepare(s authn.Session, p goldenpath.ManagementProposal) (goldenpath.ManagementInput, error) {
	verified, e := f.auth.ResolveWorkspace(f.ctx, s.Token, f.workspace)
	if e != nil {
		return goldenpath.ManagementInput{}, e
	}
	actor, e := authn.UserPrincipal(verified)
	if e != nil {
		return goldenpath.ManagementInput{}, e
	}
	plan, e := f.management.Preview(f.ctx, actor, s.Token, p)
	if e != nil {
		return goldenpath.ManagementInput{}, e
	}
	f.operation++
	return goldenpath.ManagementInput{ManagementProposal: p, ClientOperationID: fmt.Sprintf("12345678-1234-4234-8234-%012d", f.operation), PlanDigest: plan.Digest, Confirmed: true}, nil
}
func (f *membershipFixture) change(s authn.Session, p goldenpath.ManagementProposal) (goldenpath.ManagementResult, error) {
	in, e := f.prepare(s, p)
	if e != nil {
		return goldenpath.ManagementResult{}, e
	}
	return f.management.Apply(f.ctx, contextInvocation(f.principalFor(s)), s.Token, in)
}
func (f *membershipFixture) mustChange(t *testing.T, s authn.Session, p goldenpath.ManagementProposal) goldenpath.ManagementResult {
	t.Helper()
	out, e := f.change(s, p)
	if e != nil {
		var pgerr *pgconn.PgError
		if errors.As(e, &pgerr) {
			t.Fatalf("%s: %v code=%s constraint=%s message=%s", p.Kind(), e, pgerr.Code, pgerr.ConstraintName, pgerr.Message)
		}
		t.Fatalf("%s: %v", p.Kind(), e)
	}
	if out.Result != "applied" {
		t.Fatal(out)
	}
	return out
}
func proposal(kind, scope, action, user, role string) goldenpath.ManagementProposal {
	return goldenpath.ManagementProposal{ScopeKind: kind, ScopeID: scope, Action: action, TargetUserID: user, Role: role}
}

func TestMembershipPrivateManagementAndLifecycle(t *testing.T) {
	f := newMembershipFixture(t)
	b, c := f.members[0], f.members[1]
	bid, cid := b.Account.User.ID, c.Account.User.ID
	f.mustChange(t, f.owner, proposal("project", f.project, "role", bid, "admin"))
	if _, e := f.management.Members(f.ctx, f.principalFor(b), b.Token, goldenpath.ManagementQuery{ScopeKind: "channel", ScopeID: f.channel, Limit: 25}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatalf("project admin acquired private access: %v", e)
	}
	f.mustChange(t, f.owner, proposal("channel", f.channel, "add", bid, ""))
	f.mustChange(t, f.owner, proposal("channel", f.channel, "role", bid, "manager"))
	f.mustChange(t, f.owner, proposal("project", f.project, "role", bid, "viewer"))
	if _, e := f.change(b, proposal("project", f.project, "role", cid, "admin")); !errors.Is(e, authz.ErrForbidden) {
		t.Fatalf("local manager changed project role: %v", e)
	}
	if _, e := f.core.CreateMessage(f.ctx, contextInvocation(f.principalFor(b)), goldenpath.CreateMessageInput{ChannelID: f.channel, ClientOperationID: "viewer-cannot-write", Body: "must reject"}); !errors.Is(e, authz.ErrForbidden) {
		t.Fatalf("manager gained business write: %v", e)
	}
	f.mustChange(t, f.owner, proposal("thread", f.thread, "add", bid, ""))
	f.mustChange(t, f.owner, proposal("thread", f.thread, "role", bid, "manager"))
	left := f.mustChange(t, f.owner, proposal("thread", f.thread, "leave", f.principal.ID, ""))
	if _, e := f.management.Members(f.ctx, f.principal, f.owner.Token, goldenpath.ManagementQuery{ScopeKind: "thread", ScopeID: f.thread, Limit: 25}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("owner bypassed thread exit", e)
	}
	if r, e := f.management.Result(f.ctx, f.principal, f.owner.Token, left.ClientOperationID); e != nil || r.Result != "applied" {
		t.Fatal(r, e)
	}
	if _, e := f.change(f.owner, proposal("channel", f.channel, "remove", bid, "")); !errors.Is(e, authz.ErrConflict) {
		t.Fatalf("orphaned hidden thread: %v", e)
	}
	for _, kind := range []string{"project.member.remove", "channel.member.remove"} {
		scope, expectedRole := f.project, "viewer"
		if kind == "channel.member.remove" {
			scope = f.channel
		}
		_, err := f.configuration.Configure(f.ctx, contextInvocation(f.principal), goldenpath.ConfigurationInput{Kind: kind, ScopeID: scope, UserID: bid, ExpectedRole: &expectedRole, ExpectedMember: true, ClientOperationID: "legacy-" + kind})
		if !errors.Is(err, authz.ErrConflict) {
			t.Fatalf("legacy member command orphaned hidden thread: %s: %v", kind, err)
		}
	}
	if _, e := f.change(b, proposal("thread", f.thread, "add", cid, "")); !errors.Is(e, authz.ErrConflict) {
		t.Fatalf("thread granted missing parent: %v", e)
	}
	f.mustChange(t, b, proposal("channel", f.channel, "add", cid, ""))
	f.mustChange(t, b, proposal("thread", f.thread, "add", cid, ""))
	oldVerified, e := f.auth.ResolveWorkspace(f.ctx, b.Token, f.workspace)
	if e != nil {
		t.Fatal(e)
	}
	oldPrincipal, _ := authn.UserPrincipal(oldVerified)
	f.mustChange(t, f.owner, proposal("workspace", f.workspace, "suspend", bid, ""))
	if _, e = f.auth.ResolveWorkspace(f.ctx, b.Token, f.workspace); e == nil {
		t.Fatal("suspended session admitted")
	}
	f.mustChange(t, f.owner, proposal("workspace", f.workspace, "resume", bid, ""))
	if _, e = f.auth.ResolveWorkspace(f.ctx, b.Token, f.workspace); !errors.Is(e, authn.ErrRecentAuthentication) {
		t.Fatal("old session revived", e)
	}
	if _, e = f.store.ListManagementMembers(f.ctx, goldenpath.OwnerAccess{Principal: oldPrincipal, SessionDigest: sha256.Sum256([]byte(b.Token))}, goldenpath.ManagementQuery{ScopeKind: "channel", ScopeID: f.channel, Limit: 25}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("delayed admission revived", e)
	}
	b, e = f.auth.Login(f.ctx, authn.LoginInput{Email: "membership-0@example.test", Password: contextTestPassword})
	if e != nil {
		t.Fatal(e)
	}
	page, e := f.management.Members(f.ctx, f.principalFor(b), b.Token, goldenpath.ManagementQuery{ScopeKind: "thread", ScopeID: f.thread, Limit: 25})
	if e != nil || !page.CanManage {
		t.Fatal("retained manager not restored", page, e)
	}
	if _, e = f.change(b, proposal("workspace", f.workspace, "leave", bid, "")); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("sole manager left workspace", e)
	}
	f.mustChange(t, b, proposal("thread", f.thread, "role", cid, "manager"))
	f.mustChange(t, b, proposal("channel", f.channel, "role", cid, "manager"))
	removed := f.mustChange(t, b, proposal("workspace", f.workspace, "leave", bid, ""))
	if out, e := f.management.Result(f.ctx, f.principalFor(b), b.Token, removed.ClientOperationID); e != nil || out.Result != "applied" {
		t.Fatal("removed actor lost own receipt", out, e)
	}
	if out, e := f.management.Result(f.ctx, f.principalFor(c), c.Token, removed.ClientOperationID); e != nil || out.Result != "not_observed" {
		t.Fatal("other actor read receipt", out, e)
	}
	var grants int
	if e = f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM radishnexus.project_memberships WHERE user_id=$1)+(SELECT count(*) FROM radishnexus.channel_memberships WHERE user_id=$1)+(SELECT count(*) FROM radishnexus.thread_memberships WHERE user_id=$1)`, bid).Scan(&grants); e != nil || grants != 0 {
		t.Fatal("exit grants", grants, e)
	}
	general, e := f.identity.CreateInvitation(f.ctx, f.owner.Token, f.workspace)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: general.Token, SessionToken: b.Token}); e == nil {
		t.Fatal("generic invite readmitted removed user")
	}
	invite := f.mustChange(t, f.owner, proposal("workspace", f.workspace, "reinvite", bid, ""))
	if invite.Invitation == nil {
		t.Fatal("missing invitation")
	}
	if _, e = f.identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: invite.Invitation.Token, SessionToken: c.Token}); !errors.Is(e, authn.ErrInvitationInvalid) {
		t.Fatal("wrong account redeemed", e)
	}
	if _, e = f.identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: invite.Invitation.Token, SessionToken: b.Token}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.auth.ResolveWorkspace(f.ctx, b.Token, f.workspace); !errors.Is(e, authn.ErrRecentAuthentication) {
		t.Fatal("readmission revived old session", e)
	}
	b, e = f.auth.Login(f.ctx, authn.LoginInput{Email: "membership-0@example.test", Password: contextTestPassword})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.auth.ResolveWorkspace(f.ctx, b.Token, f.workspace); e != nil {
		t.Fatal(e)
	}
	if _, e = f.management.Members(f.ctx, f.principalFor(b), b.Token, goldenpath.ManagementQuery{ScopeKind: "project", ScopeID: f.project, Limit: 25}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("readmission restored old project", e)
	}
	var body string
	if e = f.pool.QueryRow(f.ctx, `SELECT body FROM radishnexus.messages WHERE id=$1`, f.message).Scan(&body); e != nil || body != "Synthetic retained evidence" {
		t.Fatal("history changed", e)
	}
}

func TestMembershipPlanRevisionAndHiddenAudit(t *testing.T) {
	f := newMembershipFixture(t)
	b, c := f.members[0], f.members[1]
	bid, cid := b.Account.User.ID, c.Account.User.ID
	in, e := f.prepare(f.owner, proposal("project", f.project, "role", bid, "admin"))
	if e != nil {
		t.Fatal(e)
	}
	f.mustChange(t, f.owner, proposal("project", f.project, "role", bid, "viewer"))
	f.mustChange(t, f.owner, proposal("project", f.project, "role", bid, "contributor"))
	if _, e = f.management.Apply(f.ctx, contextInvocation(f.principal), f.owner.Token, in); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("ABA plan applied", e)
	}
	in, e = f.prepare(f.owner, proposal("project", f.project, "role", bid, "admin"))
	if e != nil {
		t.Fatal(e)
	}
	f.mustChange(t, f.owner, proposal("channel", f.channel, "add", cid, ""))
	// A hidden membership change cannot alter the readable Project epoch.
	newPlan, e := f.management.Preview(f.ctx, f.principal, f.owner.Token, in.ManagementProposal)
	if e != nil || newPlan.Digest != in.PlanDigest {
		t.Fatal("private change affected public plan", e)
	}
	out, e := f.management.Apply(f.ctx, contextInvocation(f.principal), f.owner.Token, in)
	if e != nil {
		t.Fatal(e)
	}
	again, e := f.management.Apply(f.ctx, contextInvocation(f.principal), f.owner.Token, in)
	if e != nil || again.Result != out.Result || *again.OccurredAt != *out.OccurredAt {
		t.Fatal("idempotent retry", again, e)
	}
	in.Role = "viewer"
	if _, e = f.management.Apply(f.ctx, contextInvocation(f.principal), f.owner.Token, in); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("payload reused", e)
	}
	page, e := f.management.Audit(f.ctx, f.principalFor(b), b.Token, goldenpath.ManagementQuery{ScopeKind: "project", ScopeID: f.project, Limit: 50})
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range page.Items {
		if !strings.HasPrefix(item.Action, "management.project.") {
			t.Fatal("private audit leaked", item)
		}
	}
	if _, e = f.management.Audit(f.ctx, f.principalFor(b), b.Token, goldenpath.ManagementQuery{ScopeKind: "channel", ScopeID: f.channel, Limit: 50}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("private audit existence", e)
	}
}

func TestMembershipFailureAndCommitRecovery(t *testing.T) {
	for _, mode := range []string{"audit", "receipt", "cancel", "terminate-backend", "lost-commit"} {
		t.Run(mode, func(t *testing.T) {
			f := newMembershipFixture(t)
			in, e := f.prepare(f.owner, proposal("workspace", f.workspace, "suspend", f.members[0].Account.User.ID, ""))
			if e != nil {
				t.Fatal(e)
			}
			beforeAudit := contextCount(t, f.contextFixture, "workspace_configuration_audit")
			beforeReceipt := contextCount(t, f.contextFixture, "workspace_configuration_receipts")
			svc := f.management
			var finish func()
			if mode == "audit" || mode == "receipt" {
				table := "workspace_configuration_audit"
				if mode == "receipt" {
					table = "workspace_configuration_receipts"
				}
				contextSQL(t, f.contextFixture, `CREATE FUNCTION public.membership_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'isolated management failure'; END $$; CREATE TRIGGER membership_fault BEFORE INSERT ON radishnexus.`+table+` FOR EACH ROW EXECUTE FUNCTION public.membership_fault()`)
			}
			if mode == "lost-commit" {
				cfg := f.pool.Config().Copy()
				proxied, done := pgfault.LoseCommit(t, cfg.ConnConfig)
				cfg.ConnConfig = proxied
				cfg.MaxConns = 1
				pool, e := pgxpool.NewWithConfig(f.ctx, cfg)
				if e != nil {
					t.Fatal(e)
				}
				svc = goldenpath.NewManagementService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{})
				finish = func() { pool.Close(); done() }
			}
			if mode == "cancel" || mode == "terminate-backend" {
				release := contextGate(t, f.contextFixture, "workspace_configuration_audit")
				ctx, cancel := context.WithCancel(f.ctx)
				defer cancel()
				done := make(chan error, 1)
				go func() { _, e := svc.Apply(ctx, contextInvocation(f.principal), f.owner.Token, in); done <- e }()
				contextWaitGate(t, f.contextFixture)
				// Fixed receipt lookup must not wait on the in-flight Workspace gate.
				readCtx, stop := context.WithTimeout(f.ctx, time.Second)
				out, err := f.management.Result(readCtx, f.principal, f.owner.Token, in.ClientOperationID)
				stop()
				if err != nil || out.Result != "not_observed" {
					t.Fatal("in-flight result blocked or lied", out, err)
				}
				if mode == "cancel" {
					cancel()
				} else {
					var killed bool
					e = f.pool.QueryRow(f.ctx, `SELECT pg_terminate_backend(pid) FROM pg_locks WHERE database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND locktype='advisory' AND objid=904400 AND NOT granted`).Scan(&killed)
					if e != nil || !killed {
						t.Fatal(e)
					}
				}
				e = <-done
				release()
			} else {
				_, e = svc.Apply(f.ctx, contextInvocation(f.principal), f.owner.Token, in)
			}
			if finish != nil {
				finish()
			}
			if e == nil {
				t.Fatal("fault reported success")
			}
			out, err := f.management.Result(f.ctx, f.principal, f.owner.Token, in.ClientOperationID)
			if err != nil {
				t.Fatal(err)
			}
			var state string
			var life int64
			if err = f.pool.QueryRow(f.ctx, `SELECT status,lifecycle_revision FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2`, f.workspace, in.TargetUserID).Scan(&state, &life); err != nil {
				t.Fatal(err)
			}
			if mode == "lost-commit" {
				if !errors.Is(e, goldenpath.ErrManagementUnconfirmed) || out.Result != "applied" || state != "suspended" || life != 2 {
					t.Fatal("commit recovery", out, e, state, life)
				}
				again, err := f.management.Apply(f.ctx, contextInvocation(f.principal), f.owner.Token, in)
				if err != nil || again.Result != "applied" {
					t.Fatal(again, err)
				}
				beforeAudit++
				beforeReceipt++
			} else if out.Result != "not_observed" || state != "active" || life != 1 {
				t.Fatal("partial lifecycle commit", out, state, life, e)
			}
			if contextCount(t, f.contextFixture, "workspace_configuration_audit") != beforeAudit || contextCount(t, f.contextFixture, "workspace_configuration_receipts") != beforeReceipt {
				t.Fatal("audit/receipt cardinality")
			}
		})
	}
}

func TestMembershipResponsibilitiesAndArchivedHandoff(t *testing.T) {
	f := newMembershipFixture(t)
	b := f.members[0]
	bid := b.Account.User.ID
	page, e := f.management.Responsibilities(f.ctx, f.principal, f.owner.Token, "", 1)
	if e != nil || len(page.Items) != 1 || page.NextID == "" {
		t.Fatal(page, e)
	}
	seen := map[string]bool{page.Items[0].ID: true}
	for page.NextID != "" {
		page, e = f.management.Responsibilities(f.ctx, f.principal, f.owner.Token, page.NextID, 1)
		if e != nil {
			t.Fatal(e)
		}
		for _, i := range page.Items {
			if seen[i.ID] {
				t.Fatal("duplicate responsibility")
			}
			seen[i.ID] = true
		}
	}
	if len(seen) != 3 {
		t.Fatal("missing responsibility", seen)
	}
	other, e := f.management.Responsibilities(f.ctx, f.principalFor(b), b.Token, "", 25)
	if e != nil || len(other.Items) != 0 {
		t.Fatal("other responsibility leaked", other, e)
	}
	f.mustChange(t, f.owner, proposal("channel", f.channel, "add", bid, ""))
	f.mustChange(t, f.owner, proposal("thread", f.thread, "add", bid, ""))
	contextSQL(t, f.contextFixture, `UPDATE radishnexus.projects SET status='archived' WHERE id='`+f.project+`'`)
	if _, e = f.change(f.owner, proposal("channel", f.channel, "add", f.members[1].Account.User.ID, "")); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("archived audience expanded", e)
	}
	if _, e = f.change(f.owner, proposal("project", f.project, "leave", f.principal.ID, "")); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("last archived admin exited", e)
	}
	in := proposal("project", f.project, "handoff", bid, "")
	in.ActorAfterRole = "viewer"
	f.mustChange(t, f.owner, in)
	in = proposal("thread", f.thread, "handoff", bid, "")
	in.ActorAfterRole = "member"
	f.mustChange(t, f.owner, in)
	f.mustChange(t, f.owner, proposal("project", f.project, "leave", f.principal.ID, ""))
	if _, e = f.management.Members(f.ctx, f.principal, f.owner.Token, goldenpath.ManagementQuery{ScopeKind: "project", ScopeID: f.project, Limit: 25}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("departed owner bypass", e)
	}
}

func TestMembershipDelayedSessionAfterLogout(t *testing.T) {
	f := newMembershipFixture(t)
	b := f.members[0]
	verified, e := f.auth.ResolveWorkspace(f.ctx, b.Token, f.workspace)
	if e != nil {
		t.Fatal(e)
	}
	p, e := authn.UserPrincipal(verified)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.auth.RevokeSession(f.ctx, b.Token, b.CSRFToken); e != nil {
		t.Fatal(e)
	}
	_, e = f.core.ListRelations(f.ctx, p, entityref.Ref{Type: "thread", ID: f.thread})
	if !errors.Is(e, authn.ErrInvalidSession) {
		t.Fatal("late authorized request survived logout", e)
	}
}

func TestMembershipConcurrentLastAdminAndReadSnapshot(t *testing.T) {
	t.Run("two departures", func(t *testing.T) {
		f := newMembershipFixture(t)
		b := f.members[0]
		bid := b.Account.User.ID
		f.mustChange(t, f.owner, proposal("project", f.project, "role", bid, "admin"))
		f.mustChange(t, f.owner, proposal("channel", f.channel, "add", bid, ""))
		f.mustChange(t, f.owner, proposal("thread", f.thread, "add", bid, ""))
		a, e := f.prepare(f.owner, proposal("project", f.project, "leave", f.principal.ID, ""))
		if e != nil {
			t.Fatal(e)
		}
		bb, e := f.prepare(b, proposal("project", f.project, "leave", bid, ""))
		if e != nil {
			t.Fatal(e)
		}
		done := make(chan error, 2)
		go func() { _, e := f.management.Apply(f.ctx, contextInvocation(f.principal), f.owner.Token, a); done <- e }()
		go func() {
			_, e := f.management.Apply(f.ctx, contextInvocation(f.principalFor(b)), b.Token, bb)
			done <- e
		}()
		success, conflict := 0, 0
		for i := 0; i < 2; i++ {
			e = <-done
			if e == nil {
				success++
			} else if errors.Is(e, authz.ErrConflict) {
				conflict++
			} else {
				t.Fatal(e)
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatal(success, conflict)
		}
		var count int
		if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM radishnexus.project_memberships WHERE project_id=$1 AND role='admin'`, f.project).Scan(&count); e != nil || count != 1 {
			t.Fatal(count, e)
		}
	})
	t.Run("repeatable read waiting on lifecycle", func(t *testing.T) {
		f := newMembershipFixture(t)
		b := f.members[0]
		verified, e := f.auth.ResolveWorkspace(f.ctx, b.Token, f.workspace)
		if e != nil {
			t.Fatal(e)
		}
		p, _ := authn.UserPrincipal(verified)
		in, e := f.prepare(f.owner, proposal("workspace", f.workspace, "suspend", p.ID, ""))
		if e != nil {
			t.Fatal(e)
		}
		release := contextGate(t, f.contextFixture, "workspace_configuration_audit")
		write := make(chan error, 1)
		go func() {
			_, e := f.management.Apply(f.ctx, contextInvocation(f.principal), f.owner.Token, in)
			write <- e
		}()
		contextWaitGate(t, f.contextFixture)
		read := make(chan error, 1)
		go func() {
			_, e := goldenpath.NewDiscoveryService(f.store).ListProjects(f.ctx, p, goldenpath.DiscoveryPageInput{Limit: 25})
			read <- e
		}()
		ctx, stop := context.WithTimeout(f.ctx, 3*time.Second)
		waitProjectionLock(t, ctx, f.pool, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%19450001%')`)
		stop()
		release()
		if e = <-write; e != nil {
			t.Fatal(e)
		}
		if e = <-read; e == nil {
			t.Fatal("old snapshot survived pause")
		}
	})
}

func TestMembershipScopedSessionsAndDeploymentRevocation(t *testing.T) {
	f := newMembershipFixture(t)
	b := f.members[0]
	bid := b.Account.User.ID
	// A second Workspace isolates the same global account's admission state.
	if _, e := f.pool.Exec(f.ctx, `WITH created AS (INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_independent','Independent') RETURNING id) INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,role,status) SELECT id,$1,'member','active' FROM created`, bid); e != nil {
		t.Fatal(e)
	}
	team := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: f.workspace, Name: "Delivery management"})
	env := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "environment.create", ScopeID: f.workspace, Name: "Stage", Key: "membership-stage", OwnerTeamID: team.Object.ID, Delivery: &goldenpath.DeliveryConfigurationInput{Classification: "staging"}})
	f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "environment.authorization.grant", ScopeID: env.Object.ID, UserID: bid, Delivery: &goldenpath.DeliveryConfigurationInput{Confirmed: true}})
	f.mustChange(t, f.owner, proposal("channel", f.channel, "add", bid, ""))
	f.mustChange(t, f.owner, proposal("workspace", f.workspace, "suspend", bid, ""))
	f.mustChange(t, f.owner, proposal("channel", f.channel, "remove", bid, ""))
	f.mustChange(t, f.owner, proposal("workspace", f.workspace, "resume", bid, ""))
	if _, e := f.auth.ResolveWorkspace(f.ctx, b.Token, "wrk_independent"); e != nil {
		t.Fatal("other Workspace revoked", e)
	}
	if _, e := f.auth.ResolveWorkspace(f.ctx, b.Token, f.workspace); !errors.Is(e, authn.ErrWorkspaceAuthenticationRequired) {
		t.Fatal("old Workspace session revived", e)
	}
	var active, total int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FILTER(WHERE status='active'),count(*) FROM radishnexus.environment_deployment_authorizations WHERE workspace_id=$1 AND user_id=$2`, f.workspace, bid).Scan(&active, &total); e != nil || active != 0 || total != 1 {
		t.Fatal("deployment authority revived", active, total, e)
	}
	b, e := f.auth.Login(f.ctx, authn.LoginInput{Email: "membership-0@example.test", Password: contextTestPassword})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.management.Members(f.ctx, f.principalFor(b), b.Token, goldenpath.ManagementQuery{ScopeKind: "channel", ScopeID: f.channel, Limit: 25}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("deleted private grant revived", e)
	}
	page, e := f.management.Members(f.ctx, f.principalFor(b), b.Token, goldenpath.ManagementQuery{ScopeKind: "project", ScopeID: f.project, Limit: 25})
	if e != nil || page.SelfRole != "contributor" {
		t.Fatal("retained Project role not restored", page, e)
	}
}

func TestMembershipRepresentativeScale(t *testing.T) {
	f := newMembershipFixture(t)
	bid := f.members[0].Account.User.ID
	team := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: f.workspace, Name: "Scale team"})
	// Synthetic volume only; eligibility, plans and removals still use the same
	// production commands as the bootstrap/invitation functional fixture above.
	statements := []string{
		`INSERT INTO radishnexus.users(id,display_name) SELECT 'usr_scale_'||n,'Scale '||n FROM generate_series(1,250) n`,
		`INSERT INTO radishnexus.user_accounts(user_id,status,created_at) SELECT id,'active',clock_timestamp() FROM radishnexus.users WHERE id LIKE 'usr_scale_%'`,
		`INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,status,role) SELECT $1,id,'active','member' FROM radishnexus.users WHERE id LIKE 'usr_scale_%'`,
		`INSERT INTO radishnexus.projects(id,workspace_id,key,name,owner_team_id,visibility,status,created_by_kind) SELECT 'prj_scale_'||n,$1,'scale-'||n,'Scale project '||n,$2,'restricted','active','system' FROM generate_series(1,200) n`,
		`INSERT INTO radishnexus.project_memberships(workspace_id,project_id,user_id,role) SELECT $1,p.id,u,'admin' FROM radishnexus.projects p CROSS JOIN unnest(ARRAY[$3::text,$4::text]) u WHERE p.id LIKE 'prj_scale_%'`,
		`INSERT INTO radishnexus.channels(id,workspace_id,governing_project_id,name,visibility,status,created_by_kind) SELECT 'chn_scale_'||n,$1,'prj_scale_'||n,'Scale channel '||n,'restricted','active','system' FROM generate_series(1,200) n`,
		`INSERT INTO radishnexus.channel_memberships(workspace_id,channel_id,user_id,management_role) SELECT $1,c.id,u,'manager' FROM radishnexus.channels c CROSS JOIN unnest(ARRAY[$3::text,$4::text]) u WHERE c.id LIKE 'chn_scale_%'`,
		`INSERT INTO radishnexus.threads(id,workspace_id,governing_project_id,origin_channel_id,title,visibility,created_by) SELECT 'thr_scale_'||n,$1,'prj_scale_'||n,NULL,'Scale thread '||n,'restricted',$3 FROM generate_series(1,200) n`,
		`INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id,management_role) SELECT $1,t.id,u,'manager' FROM radishnexus.threads t CROSS JOIN unnest(ARRAY[$3::text,$4::text]) u WHERE t.id LIKE 'thr_scale_%'`,
	}
	for _, sql := range statements {
		// Substitute only generated fixture IDs into the synthetic volume seed.
		sql = strings.ReplaceAll(sql, "$1", "'"+f.workspace+"'")
		sql = strings.ReplaceAll(sql, "$2", "'"+team.Object.ID+"'")
		sql = strings.ReplaceAll(sql, "$3", "'"+f.principal.ID+"'")
		sql = strings.ReplaceAll(sql, "$4", "'"+bid+"'")
		if _, e := f.pool.Exec(f.ctx, sql); e != nil {
			t.Fatal(e)
		}
	}
	cfg := f.pool.Config().Copy()
	counter := &relationQueryCounter{}
	cfg.ConnConfig.Tracer = counter
	pool, e := pgxpool.NewWithConfig(f.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	svc := goldenpath.NewManagementService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{})
	lifecycleStart := time.Now()
	f.mustChange(t, f.owner, proposal("workspace", f.workspace, "suspend", bid, ""))
	pauseTime := time.Since(lifecycleStart)
	lifecycleStart = time.Now()
	f.mustChange(t, f.owner, proposal("workspace", f.workspace, "resume", bid, ""))
	resumeTime := time.Since(lifecycleStart)
	started := time.Now()
	page, e := svc.Members(f.ctx, f.principal, f.owner.Token, goldenpath.ManagementQuery{ScopeKind: "workspace", ScopeID: f.workspace, Limit: 50})
	readTime := time.Since(started)
	readQueries := counter.queries.Load()
	if e != nil || len(page.Items) != 50 || page.NextID == "" {
		t.Fatal(page, e)
	}
	counter.queries.Store(0)
	started = time.Now()
	p, e := svc.Preview(f.ctx, f.principal, f.owner.Token, proposal("workspace", f.workspace, "remove", bid, ""))
	previewTime := time.Since(started)
	previewQueries := counter.queries.Load()
	if e != nil {
		t.Fatal(e)
	}
	in := goldenpath.ManagementInput{ManagementProposal: p.ManagementProposal, ClientOperationID: "12345678-1234-4234-8234-000000999999", PlanDigest: p.Digest, Confirmed: true}
	counter.queries.Store(0)
	started = time.Now()
	out, e := svc.Apply(f.ctx, contextInvocation(f.principal), f.owner.Token, in)
	applyTime := time.Since(started)
	applyQueries := counter.queries.Load()
	if e != nil || out.Result != "applied" {
		t.Fatal(out, e)
	}
	if applyQueries > 40 {
		t.Fatal("management query count grew with descendant count", applyQueries)
	}
	var remaining int
	if e = f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM radishnexus.project_memberships WHERE user_id=$1)+(SELECT count(*) FROM radishnexus.channel_memberships WHERE user_id=$1)+(SELECT count(*) FROM radishnexus.thread_memberships WHERE user_id=$1)`, bid).Scan(&remaining); e != nil || remaining != 0 {
		t.Fatal("truncated cleanup", remaining, e)
	}
	t.Logf("254 members; 201 projects / channels / threads; 600 affected managed scopes: roster=%s/%d SQL, preview=%s/%d SQL, remove=%s/%d SQL", readTime, readQueries, previewTime, previewQueries, applyTime, applyQueries)
	t.Logf("same fixture lifecycle preview+apply: pause=%s, resume=%s", pauseTime, resumeTime)
}
