//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestMembershipPauseCompetition(t *testing.T) {
	for _, mode := range []string{"invitation", "owner-handoff", "local-role", "deployment-grant", "recovery", "logout"} {
		t.Run(mode, func(t *testing.T) {
			f := newMembershipFixture(t)
			b := f.members[0]
			bid := b.Account.User.ID
			environment := ""
			if mode == "deployment-grant" {
				team := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: f.workspace, Name: "Competing deployment"})
				env := f.configure(t, f.principal, goldenpath.ConfigurationInput{Kind: "environment.create", ScopeID: f.workspace, Name: "Competing stage", Key: "competing-stage", OwnerTeamID: team.Object.ID, Delivery: &goldenpath.DeliveryConfigurationInput{Classification: "staging"}})
				environment = env.Object.ID
			}
			invite, e := f.identity.CreateInvitation(f.ctx, f.owner.Token, f.workspace)
			if e != nil {
				t.Fatal(e)
			}
			f.mustChange(t, f.owner, proposal("channel", f.channel, "add", bid, ""))
			local, e := f.prepare(f.owner, proposal("channel", f.channel, "role", bid, "manager"))
			if e != nil {
				t.Fatal(e)
			}
			pause, e := f.prepare(f.owner, proposal("workspace", f.workspace, "suspend", bid, ""))
			if e != nil {
				t.Fatal(e)
			}
			recovery := authn.NewRecoveryService(authpostgres.New(f.pool), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{})
			inspect, e := recovery.Inspect(f.ctx, "membership-0@example.test")
			if e != nil {
				t.Fatal(e)
			}
			release := contextGate(t, f.contextFixture, "workspace_configuration_audit")
			done := make(chan error, 1)
			go func() {
				_, e := f.management.Apply(f.ctx, contextInvocation(f.principal), f.owner.Token, pause)
				done <- e
			}()
			contextWaitGate(t, f.contextFixture)
			later := make(chan error, 1)
			go func() {
				var err error
				switch mode {
				case "invitation":
					_, err = f.identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: invite.Token, SessionToken: b.Token})
				case "owner-handoff":
					_, err = goldenpath.NewOwnerTransferService(f.store, goldenpath.CryptoIDGenerator{}).Transfer(f.ctx, contextInvocation(f.principal), f.owner.Token, goldenpath.OwnerTransferInput{ClientOperationID: "99999999-1234-4234-8234-000000000001", TargetUserID: bid, ExpectedActorRoleRevision: "1", ExpectedTargetRoleRevision: "1", Confirmed: true})
				case "local-role":
					_, err = f.management.Apply(f.ctx, contextInvocation(f.principal), f.owner.Token, local)
				case "deployment-grant":
					_, err = f.configuration.Configure(f.ctx, contextInvocation(f.principal), goldenpath.ConfigurationInput{Kind: "environment.authorization.grant", ScopeID: environment, UserID: bid, ClientOperationID: "competing-grant", Delivery: &goldenpath.DeliveryConfigurationInput{Confirmed: true}})
				case "recovery":
					_, err = recovery.Apply(f.ctx, authn.RecoveryInput{UserID: bid, ExpectedEmail: "membership-0@example.test", ExpectedPasswordChangedAt: inspect.PasswordChangedAt, OperationID: "99999999-1234-4234-8234-000000000002", OperatorRef: "membership_test", ReasonCode: "lost_local_password", NewPassword: "new synthetic membership recovery password", Confirmed: true})
				case "logout":
					err = f.auth.RevokeSession(f.ctx, f.owner.Token, f.owner.CSRFToken)
				}
				later <- err
			}()
			// Observe a real competing PostgreSQL lock, not a scheduling sleep.
			waitCtx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
			waitProjectionLock(t, waitCtx, f.pool, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query NOT LIKE '%904400%' AND query NOT LIKE '%pg_stat_activity%')`)
			cancel()
			release()
			if e = <-done; e != nil {
				t.Fatal(e)
			}
			e = <-later
			if mode == "recovery" || mode == "logout" {
				if e != nil {
					t.Fatal(e)
				}
			} else if e == nil {
				t.Fatal("paused target accepted competing action")
			}
			if _, e = f.auth.ResolveWorkspace(f.ctx, b.Token, f.workspace); e == nil {
				t.Fatal("old session admitted")
			}
			var status, role string
			if e = f.pool.QueryRow(f.ctx, `SELECT status,role FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2`, f.workspace, bid).Scan(&status, &role); e != nil || status != "suspended" || role != "member" {
				t.Fatal(status, role, e)
			}
		})
	}
}

type membershipChildInput struct {
	Database, Token string
	Principal       authz.Principal
	Input           goldenpath.ManagementInput
}

func TestMembershipProcessInterruption(t *testing.T) {
	if os.Getenv("RADISHNEXUS_MEMBERSHIP_CHILD") == "1" {
		var input membershipChildInput
		if e := json.NewDecoder(os.Stdin).Decode(&input); e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		pool, e := pgxpool.New(ctx, input.Database)
		if e != nil {
			t.Fatal(e)
		}
		defer pool.Close()
		_, e = goldenpath.NewManagementService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{}).Apply(ctx, contextInvocation(input.Principal), input.Token, input.Input)
		if e != nil {
			t.Fatal(e)
		}
		return
	}
	f := newMembershipFixture(t)
	b := f.members[0]
	in, e := f.prepare(f.owner, proposal("workspace", f.workspace, "suspend", b.Account.User.ID, ""))
	if e != nil {
		t.Fatal(e)
	}
	beforeAudit := contextCount(t, f.contextFixture, "workspace_configuration_audit")
	release := contextGate(t, f.contextFixture, "workspace_configuration_audit")
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	// ConnString preserves the original DSN; contextDatabase changes Database
	// on the parsed config to isolate this fixture. Carry that actual database.
	database, e := url.Parse(f.pool.Config().ConnString())
	if e != nil {
		t.Fatal(e)
	}
	database.Path = "/" + f.pool.Config().ConnConfig.Database
	raw, e := json.Marshal(membershipChildInput{database.String(), f.owner.Token, f.principal, in})
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(executable, "-test.run=^TestMembershipProcessInterruption$")
	cmd.Env = append(os.Environ(), "RADISHNEXUS_MEMBERSHIP_CHILD=1")
	cmd.Stdin = bytes.NewReader(raw)
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	contextWaitGate(t, f.contextFixture)
	if e = cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	if e = cmd.Wait(); e == nil {
		t.Fatal("child was not interrupted")
	}
	release()
	out, e := f.management.Result(f.ctx, f.principal, f.owner.Token, in.ClientOperationID)
	if e != nil || out.Result != "not_observed" {
		t.Fatal(out, e)
	}
	var status string
	var revision int64
	if e = f.pool.QueryRow(f.ctx, `SELECT status,lifecycle_revision FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2`, f.workspace, b.Account.User.ID).Scan(&status, &revision); e != nil || status != "active" || revision != 1 {
		t.Fatal(status, revision, e)
	}
	if contextCount(t, f.contextFixture, "workspace_configuration_audit") != beforeAudit {
		t.Fatal("partial audit")
	}
}

func TestMembershipOldReadmissionAndDisabledRemoval(t *testing.T) {
	f := newMembershipFixture(t)
	b := f.members[0]
	bid := b.Account.User.ID
	f.mustChange(t, b, proposal("workspace", f.workspace, "leave", bid, ""))
	old := f.mustChange(t, f.owner, proposal("workspace", f.workspace, "reinvite", bid, ""))
	fresh := f.mustChange(t, f.owner, proposal("workspace", f.workspace, "reinvite", bid, ""))
	if _, e := f.identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: fresh.Invitation.Token, SessionToken: b.Token}); e != nil {
		t.Fatal(e)
	}
	b, e := f.auth.Login(f.ctx, authn.LoginInput{Email: "membership-0@example.test", Password: contextTestPassword})
	if e != nil {
		t.Fatal(e)
	}
	f.mustChange(t, b, proposal("workspace", f.workspace, "leave", bid, ""))
	if _, e = f.identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: old.Invitation.Token, SessionToken: b.Token}); !errors.Is(e, authn.ErrInvitationInvalid) {
		t.Fatal("old exit generation redeemed", e)
	}
	cid := f.members[1].Account.User.ID
	if _, e = f.pool.Exec(f.ctx, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, cid); e != nil {
		t.Fatal(e)
	}
	f.mustChange(t, f.owner, proposal("workspace", f.workspace, "remove", cid, ""))
	if _, e = f.change(f.owner, proposal("workspace", f.workspace, "reinvite", cid, "")); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("disabled account reinvited", e)
	}
}

func TestMembershipImportedIdentityBoundary(t *testing.T) {
	source := contextDatabase(t)
	source.seed(t)
	bundle := source.bundle(t)
	target := contextDatabase(t)
	plan := target.plan(t, bundle)
	result, e := target.service.Apply(target.ctx, target.principal, target.owner.Token, bundle, plan, plan.PlanDigest)
	if e != nil {
		t.Fatal(e)
	}
	management := goldenpath.NewManagementService(target.store, goldenpath.CryptoIDGenerator{})
	var historical, channel, thread string
	if e = target.pool.QueryRow(target.ctx, `SELECT target_id FROM radishnexus.context_import_mappings WHERE kind='actor' ORDER BY target_id LIMIT 1`).Scan(&historical); e != nil {
		t.Fatal(e)
	}
	if e = target.pool.QueryRow(target.ctx, `SELECT id FROM radishnexus.channels WHERE governing_project_id=$1 AND visibility='restricted' LIMIT 1`, result.ProjectID).Scan(&channel); e != nil {
		t.Fatal(e)
	}
	if e = target.pool.QueryRow(target.ctx, `SELECT id FROM radishnexus.threads WHERE governing_project_id=$1 AND visibility='restricted' LIMIT 1`, result.ProjectID).Scan(&thread); e != nil {
		t.Fatal(e)
	}
	for _, scope := range []struct{ kind, id string }{{"channel", channel}, {"thread", thread}} {
		page, e := management.Members(target.ctx, target.principal, target.owner.Token, goldenpath.ManagementQuery{ScopeKind: scope.kind, ScopeID: scope.id, Limit: 50})
		if e != nil || !page.CanManage || page.SelfRole != "manager" {
			t.Fatal("importer missing local responsibility", page, e)
		}
		candidates, e := management.Members(target.ctx, target.principal, target.owner.Token, goldenpath.ManagementQuery{ScopeKind: scope.kind, ScopeID: scope.id, Limit: 50, Candidates: true})
		if e != nil {
			t.Fatal(e)
		}
		for _, item := range candidates.Items {
			if item.ID == historical {
				t.Fatal("historical actor is a candidate")
			}
		}
		if _, e = management.Preview(target.ctx, target.principal, target.owner.Token, proposal(scope.kind, scope.id, "add", historical, "")); !errors.Is(e, authz.ErrNotFound) {
			t.Fatal("historical actor admitted", e)
		}
	}
	for _, action := range []string{"resume", "remove", "reinvite"} {
		if _, e = management.Preview(target.ctx, target.principal, target.owner.Token, proposal("workspace", target.workspace, action, historical, "")); !errors.Is(e, authz.ErrNotFound) {
			t.Fatal("historical actor mutated", action, e)
		}
	}
	if _, e = target.service.Export(target.ctx, target.principal, target.owner.Token, result.ProjectID); e != nil {
		t.Fatal("re-export changed profile", e)
	}
	var active bool
	if e = target.pool.QueryRow(target.ctx, `SELECT status='active' FROM radishnexus.user_accounts WHERE user_id=$1`, historical).Scan(&active); e != nil || active {
		t.Fatal("history identity activated", e)
	}
}

func TestMembershipIndependentWorkspaceWhileWriteBlocked(t *testing.T) {
	f := newMembershipFixture(t)
	if _, e := f.pool.Exec(f.ctx, `WITH w AS(INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_independent','Independent') RETURNING id) INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,role,status) SELECT id,$1,'owner','active' FROM w`, f.principal.ID); e != nil {
		t.Fatal(e)
	}
	in, e := f.prepare(f.owner, proposal("workspace", f.workspace, "suspend", f.members[0].Account.User.ID, ""))
	if e != nil {
		t.Fatal(e)
	}
	release := contextGate(t, f.contextFixture, "workspace_configuration_audit")
	done := make(chan error, 1)
	go func() {
		_, e := f.management.Apply(f.ctx, contextInvocation(f.principal), f.owner.Token, in)
		done <- e
	}()
	contextWaitGate(t, f.contextFixture)
	p := f.principal
	p.WorkspaceID = "wrk_independent"
	ctx, cancel := context.WithTimeout(f.ctx, time.Second)
	defer cancel()
	started := time.Now()
	page, e := f.management.Members(ctx, p, f.owner.Token, goldenpath.ManagementQuery{ScopeKind: "workspace", ScopeID: p.WorkspaceID, Limit: 25})
	if e != nil || len(page.Items) != 1 {
		t.Fatal("independent workspace blocked", e)
	}
	t.Logf("independent Workspace roster during held mutation gate: %s", time.Since(started))
	release()
	if e = <-done; e != nil {
		t.Fatal(e)
	}
}

func TestMembershipIssuerLifecycleAndOIDCInvitation(t *testing.T) {
	f := newMembershipFixture(t)
	b := f.members[0]
	invite, e := f.identity.CreateInvitation(f.ctx, f.owner.Token, f.workspace)
	if e != nil {
		t.Fatal(e)
	}
	// Represent an existing multi-owner deployment; no new owner-grant endpoint.
	if _, e = f.pool.Exec(f.ctx, `UPDATE radishnexus.workspace_memberships SET role='owner',role_revision=role_revision+1 WHERE workspace_id=$1 AND user_id=$2`, f.workspace, b.Account.User.ID); e != nil {
		t.Fatal(e)
	}
	f.mustChange(t, b, proposal("workspace", f.workspace, "suspend", f.principal.ID, ""))
	f.mustChange(t, b, proposal("workspace", f.workspace, "resume", f.principal.ID, ""))
	if _, e = f.identity.AcceptInvitation(f.ctx, authn.AcceptInvitationInput{InvitationToken: invite.Token, SessionToken: b.Token}); !errors.Is(e, authn.ErrInvitationInvalid) {
		t.Fatal("issuer lifecycle revived invitation", e)
	}
	digest := sha256.Sum256([]byte(invite.Token))
	now := time.Now()
	completion := authn.OIDCCompletion{Transaction: authn.OIDCTransaction{InvitationDigest: digest[:], DisplayName: "Synthetic federated member", ExpiresAt: now.Add(time.Minute)}, Identity: authn.ExternalIdentity{Issuer: "https://identity.example.test", Subject: "membership-lifecycle"}, NewUserID: "usr_federated_membership", Session: authn.SessionRecord{ID: "ses_federated_membership", CreatedAt: now, ExpiresAt: now.Add(authn.SessionLifetime)}, AuditID: "ida_federated_membership"}
	if e = authpostgres.New(f.pool).CompleteOIDC(f.ctx, completion); !errors.Is(e, authn.ErrInvitationInvalid) {
		t.Fatal("OIDC revived issuer lifecycle", e)
	}
	var created bool
	if e = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM radishnexus.users WHERE id='usr_federated_membership')`).Scan(&created); e != nil || created {
		t.Fatal("failed invitation created identity", e)
	}
}

func TestMembershipStaleHandoffAndSecurityPause(t *testing.T) {
	for _, change := range []string{"target-lifecycle", "parent-membership", "actor-role"} {
		t.Run(change, func(t *testing.T) {
			f := newMembershipFixture(t)
			b, c := f.members[0], f.members[1]
			bid, cid := b.Account.User.ID, c.Account.User.ID
			f.mustChange(t, f.owner, proposal("channel", f.channel, "add", bid, ""))
			f.mustChange(t, f.owner, proposal("channel", f.channel, "add", cid, ""))
			f.mustChange(t, f.owner, proposal("thread", f.thread, "add", bid, ""))
			f.mustChange(t, f.owner, proposal("thread", f.thread, "add", cid, ""))
			actor, target := f.owner, bid
			if change == "actor-role" {
				f.mustChange(t, f.owner, proposal("thread", f.thread, "role", bid, "manager"))
				actor, target = b, cid
			}
			p := proposal("thread", f.thread, "handoff", target, "")
			p.ActorAfterRole = "member"
			in, err := f.prepare(actor, p)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "target-lifecycle":
				f.mustChange(t, f.owner, proposal("workspace", f.workspace, "suspend", bid, ""))
				f.mustChange(t, f.owner, proposal("workspace", f.workspace, "resume", bid, ""))
			case "parent-membership":
				f.mustChange(t, f.owner, proposal("channel", f.channel, "remove", bid, ""))
				f.mustChange(t, f.owner, proposal("channel", f.channel, "add", bid, ""))
				f.mustChange(t, f.owner, proposal("thread", f.thread, "add", bid, ""))
			case "actor-role":
				f.mustChange(t, f.owner, proposal("thread", f.thread, "role", bid, "member"))
			}
			_, err = f.management.Apply(f.ctx, contextInvocation(f.principalFor(actor)), actor.Token, in)
			if !errors.Is(err, authz.ErrConflict) && !errors.Is(err, authz.ErrForbidden) {
				t.Fatalf("stale handoff succeeded: %v", err)
			}
		})
	}
	t.Run("sole project and private manager may be paused", func(t *testing.T) {
		f := newMembershipFixture(t)
		b := f.members[0]
		bid := b.Account.User.ID
		f.mustChange(t, f.owner, proposal("channel", f.channel, "add", bid, ""))
		f.mustChange(t, f.owner, proposal("thread", f.thread, "add", bid, ""))
		// Exit the original admin through one atomic handoff; the recipient is
		// now the only Project admin and effective private manager.
		f.mustChange(t, f.owner, proposal("project", f.project, "handoff", bid, ""))
		f.mustChange(t, f.owner, proposal("workspace", f.workspace, "suspend", bid, ""))
		if _, err := f.management.Members(f.ctx, f.principal, f.owner.Token, goldenpath.ManagementQuery{ScopeKind: "channel", ScopeID: f.channel, Limit: 25}); !errors.Is(err, authz.ErrNotFound) {
			t.Fatalf("owner acquired private takeover: %v", err)
		}
		if _, err := f.change(f.owner, proposal("workspace", f.workspace, "leave", f.principal.ID, "")); !errors.Is(err, authz.ErrConflict) {
			t.Fatalf("last Workspace owner left: %v", err)
		}
		f.mustChange(t, f.owner, proposal("workspace", f.workspace, "resume", bid, ""))
		b, err := f.auth.Login(f.ctx, authn.LoginInput{Email: "membership-0@example.test", Password: contextTestPassword})
		if err != nil {
			t.Fatal(err)
		}
		page, err := f.management.Members(f.ctx, f.principalFor(b), b.Token, goldenpath.ManagementQuery{ScopeKind: "thread", ScopeID: f.thread, Limit: 25})
		if err != nil || !page.CanManage {
			t.Fatalf("responsibility did not resume: %v %+v", err, page)
		}
	})
}
