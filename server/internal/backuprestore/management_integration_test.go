//go:build integration

package backuprestore

import (
	"context"
	"errors"
	"fmt"
	"github.com/laugh0608/RadishNexus/server/db"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBackupRestoreMembershipLifecycle(t *testing.T) {
	sourceURL, targetURL := os.Getenv("SOURCE_DATABASE_URL"), os.Getenv("TARGET_DATABASE_URL")
	if sourceURL == "" || targetURL == "" {
		t.Skip("source and target required")
	}
	ctx := context.Background()
	sourceURL = recoveryBackupDatabase(t, ctx, sourceURL)
	targetURL = recoveryBackupDatabase(t, ctx, targetURL)
	source := connectIntegrationDatabase(t, ctx, sourceURL)
	defer source.Close(ctx)
	target := connectIntegrationDatabase(t, ctx, targetURL)
	defer target.Close(ctx)
	if e := db.Migrate(ctx, source); e != nil {
		t.Fatal(e)
	}
	pool := connectIntegrationPool(t, ctx, sourceURL)
	defer pool.Close()
	auth := authn.NewService(authpostgres.New(pool), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	const password = "membership restore synthetic password"
	boot, e := auth.Bootstrap(ctx, authn.BootstrapInput{Email: "owner@example.test", Password: password, DisplayName: "Owner", WorkspaceName: "Membership restore"})
	if e != nil {
		t.Fatal(e)
	}
	owner, e := auth.Login(ctx, authn.LoginInput{Email: "owner@example.test", Password: password})
	if e != nil {
		t.Fatal(e)
	}
	identity := authn.NewIdentityService(authpostgres.New(pool), auth, "")
	invite, e := identity.CreateInvitation(ctx, owner.Token, boot.WorkspaceID)
	if e != nil {
		t.Fatal(e)
	}
	member, e := identity.AcceptInvitation(ctx, authn.AcceptInvitationInput{InvitationToken: invite.Token, Email: "member@example.test", Password: password, DisplayName: "Member"})
	if e != nil {
		t.Fatal(e)
	}
	actor := authz.Principal{Kind: authz.PrincipalUser, ID: boot.UserID, WorkspaceID: boot.WorkspaceID}
	store := goldenpostgres.New(pool)
	configure := goldenpath.NewConfigurationService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	management := goldenpath.NewManagementService(store, goldenpath.CryptoIDGenerator{})
	sequence := 0
	config := func(in goldenpath.ConfigurationInput) goldenpath.ConfigurationResult {
		sequence++
		in.ClientOperationID = fmt.Sprintf("restore-%d", sequence)
		out, e := configure.Configure(ctx, goldenpath.Invocation{Principal: actor, SourceKind: "web", CorrelationID: "membership-restore"}, in)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	change := func(kind, id, action, user, role string) goldenpath.ManagementResult {
		p := goldenpath.ManagementProposal{ScopeKind: kind, ScopeID: id, Action: action, TargetUserID: user, Role: role}
		plan, e := management.Preview(ctx, actor, owner.Token, p)
		if e != nil {
			t.Fatal(e)
		}
		sequence++
		out, e := management.Apply(ctx, goldenpath.Invocation{Principal: actor}, owner.Token, goldenpath.ManagementInput{ManagementProposal: p, ClientOperationID: fmt.Sprintf("12345678-1234-4234-8234-%012d", sequence), PlanDigest: plan.Digest, Confirmed: true})
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	team := config(goldenpath.ConfigurationInput{Kind: "team.create", ScopeID: boot.WorkspaceID, Name: "Restore team"})
	project := config(goldenpath.ConfigurationInput{Kind: "project.create", ScopeID: boot.WorkspaceID, Name: "Private restore", Key: "restore", OwnerTeamID: team.Object.ID, Visibility: "restricted", InitialAdminUserID: actor.ID})
	change("project", project.Object.ID, "add", member.Account.User.ID, "viewer")
	channel := config(goldenpath.ConfigurationInput{Kind: "channel.create", ScopeID: project.Object.ID, Name: "Private restore", Visibility: "restricted", MemberUserIDs: []string{actor.ID}})
	change("channel", channel.Object.ID, "add", member.Account.User.ID, "")
	change("channel", channel.Object.ID, "role", member.Account.User.ID, "manager")
	change("workspace", boot.WorkspaceID, "suspend", member.Account.User.ID, "")
	result := change("workspace", boot.WorkspaceID, "resume", member.Account.User.ID, "")
	baseline := snapshotIncludedTables(t, ctx, pool)
	path := filepath.Join(t.TempDir(), "membership-backup")
	backup := NewService()
	if _, e = backup.Backup(ctx, source, sourceURL, path); e != nil {
		t.Fatal(e)
	}
	if _, e = backup.Restore(ctx, target, targetURL, path); e != nil {
		t.Fatal(e)
	}
	restored := connectIntegrationPool(t, ctx, targetURL)
	defer restored.Close()
	if !reflect.DeepEqual(baseline, snapshotIncludedTables(t, ctx, restored)) {
		t.Fatal("lifecycle, manager, audit or receipt restore drift")
	}
	restoredAuth := authn.NewService(authpostgres.New(restored), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	if _, e = restoredAuth.ResolveSession(ctx, member.Token); !errors.Is(e, authn.ErrInvalidSession) {
		t.Fatal("backup carried Session", e)
	}
	fresh, e := restoredAuth.Login(ctx, authn.LoginInput{Email: "member@example.test", Password: password})
	if e != nil {
		t.Fatal(e)
	}
	verified, e := restoredAuth.ResolveWorkspace(ctx, fresh.Token, boot.WorkspaceID)
	if e != nil || verified.MembershipRevision != 3 {
		t.Fatal("restored lifecycle admission", verified, e)
	}
	p, e := authn.UserPrincipal(verified)
	if e != nil {
		t.Fatal(e)
	}
	restoredManagement := goldenpath.NewManagementService(goldenpostgres.New(restored), goldenpath.CryptoIDGenerator{})
	page, e := restoredManagement.Members(ctx, p, fresh.Token, goldenpath.ManagementQuery{ScopeKind: "channel", ScopeID: channel.Object.ID, Limit: 25})
	if e != nil || !page.CanManage || page.SelfRole != "manager" {
		t.Fatal("restored manager", page, e)
	}
	owner, e = restoredAuth.Login(ctx, authn.LoginInput{Email: "owner@example.test", Password: password})
	if e != nil {
		t.Fatal(e)
	}
	saved, e := restoredManagement.Result(ctx, actor, owner.Token, result.ClientOperationID)
	if e != nil || saved.Result != "applied" || saved.Action != result.Action {
		t.Fatal("restored receipt", saved, e)
	}
}
