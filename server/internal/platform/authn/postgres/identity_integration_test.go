//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

// This provider tests transaction/admission behavior only; cryptographic
// verification is exercised separately by the real provider HTTPS tests.
type identityTestProvider struct{}

func (identityTestProvider) Issuer() string { return "https://radish.example.test" }
func (identityTestProvider) Fingerprint() []byte {
	h := sha256.Sum256([]byte("reviewed-test-registration"))
	return h[:]
}
func (identityTestProvider) AuthorizationURL(_ context.Context, state, nonce, verifier string) (string, error) {
	return "https://radish.example.test/connect/authorize?" + url.Values{"state": {state}, "nonce": {nonce}, "verifier_for_test": {verifier}}.Encode(), nil
}
func (identityTestProvider) Exchange(_ context.Context, code, verifier string, nonce []byte) (authn.ExternalIdentity, error) {
	if code == "fail" {
		return authn.ExternalIdentity{}, authn.ErrOIDCInvalid
	}
	return authn.ExternalIdentity{Issuer: "https://radish.example.test", Subject: code}, nil
}

func assertIdentityAdmissionAndFederation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID string, now time.Time) {
	t.Helper()
	store := authpostgres.New(pool)
	auth := authn.NewService(store, authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, integrationClock{now: now})
	identity := authn.NewIdentityService(store, auth, "https://radish.example.test")
	oidc := authn.NewOIDCService(store, identity, identityTestProvider{})
	owner, err := auth.Login(ctx, authn.LoginInput{Email: "admin@example.test", Password: "correct horse battery staple"})
	if err != nil {
		t.Fatal(err)
	}
	invite := func() authn.Invitation {
		t.Helper()
		value, err := identity.CreateInvitation(ctx, owner.Token, workspaceID)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	acceptance := func(token, email string) authn.AcceptInvitationInput {
		return authn.AcceptInvitationInput{InvitationToken: token, Email: email, DisplayName: "Invited member", Password: "invited member password"}
	}
	first := invite()
	member, err := identity.AcceptInvitation(ctx, acceptance(first.Token, "member@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	if len(member.Account.Workspaces) != 1 || member.Account.Workspaces[0].Role != "member" {
		t.Fatal("admission granted unexpected role")
	}
	if _, err := identity.AcceptInvitation(ctx, acceptance(first.Token, "replay@example.test")); !errors.Is(err, authn.ErrInvitationInvalid) {
		t.Fatal("invitation replay accepted", err)
	}
	if _, err := identity.CreateInvitation(ctx, member.Token, workspaceID); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("member created invitation", err)
	}
	existingInvite := invite()
	joined, err := identity.AcceptInvitation(ctx, authn.AcceptInvitationInput{InvitationToken: existingInvite.Token, SessionToken: member.Token})
	if err != nil || joined.Token != "" || joined.Account.User.ID != member.Account.User.ID {
		t.Fatal("existing account invitation changed identity or renewed session", err)
	}
	suspendedInvite := invite()
	if _, err := pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE workspace_id=$1 AND user_id=$2`, workspaceID, member.Account.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.AcceptInvitation(ctx, authn.AcceptInvitationInput{InvitationToken: suspendedInvite.Token, SessionToken: member.Token}); !errors.Is(err, authz.ErrForbidden) {
		t.Fatal("invitation reactivated suspended member", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET status='active' WHERE workspace_id=$1 AND user_id=$2`, workspaceID, member.Account.User.ID); err != nil {
		t.Fatal(err)
	}
	conflict := invite()
	if _, err := identity.AcceptInvitation(ctx, acceptance(conflict.Token, "MEMBER@EXAMPLE.TEST")); !errors.Is(err, authn.ErrIdentityConflict) {
		t.Fatal("email merged an account", err)
	}
	if _, err := identity.AcceptInvitation(ctx, acceptance(conflict.Token, "second@example.test")); err != nil {
		t.Fatal("failed acceptance consumed invitation", err)
	}

	revoked := invite()
	if _, err := pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET role='member' WHERE workspace_id=$1 AND user_id=$2`, workspaceID, owner.Account.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.AcceptInvitation(ctx, acceptance(revoked.Token, "revoked@example.test")); !errors.Is(err, authn.ErrInvitationInvalid) {
		t.Fatal("revoked inviter admitted member", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, workspaceID, owner.Account.User.ID); err != nil {
		t.Fatal(err)
	}
	concurrent := invite()
	outcomes := make(chan error, 2)
	var wg sync.WaitGroup
	for _, email := range []string{"race-a@example.test", "race-b@example.test"} {
		wg.Add(1)
		go func(email string) {
			defer wg.Done()
			_, err := identity.AcceptInvitation(ctx, acceptance(concurrent.Token, email))
			outcomes <- err
		}(email)
	}
	wg.Wait()
	close(outcomes)
	successes := 0
	for err := range outcomes {
		if err == nil {
			successes++
		} else if !errors.Is(err, authn.ErrInvitationInvalid) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("concurrent invitation did not have one winner")
	}

	start := func(input authn.OIDCStartInput) (authn.OIDCStart, string) {
		t.Helper()
		value, err := oidc.Start(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(value.AuthorizationURL)
		if err != nil {
			t.Fatal(err)
		}
		return value, u.Query().Get("state")
	}
	denied, state := start(authn.OIDCStartInput{Mode: "login"})
	if _, _, err := oidc.Callback(ctx, state, denied.BrowserToken, "unknown", false); !errors.Is(err, authn.ErrInvitationInvalid) {
		t.Fatal("uninvited OIDC account admitted", err)
	}
	oidcInvite := invite()
	login, state := start(authn.OIDCStartInput{Mode: "login", InvitationToken: oidcInvite.Token, DisplayName: "External member"})
	if _, _, err := oidc.Callback(ctx, state, integrationToken(99), "external-subject", false); !errors.Is(err, authn.ErrOIDCInvalid) {
		t.Fatal("foreign browser accepted", err)
	}
	external, linked, err := oidc.Callback(ctx, state, login.BrowserToken, "external-subject", false)
	if err != nil || linked {
		t.Fatal("OIDC admission failed", err)
	}
	if _, _, err := oidc.Callback(ctx, state, login.BrowserToken, "external-subject", false); !errors.Is(err, authn.ErrOIDCInvalid) {
		t.Fatal("OIDC callback replay accepted", err)
	}
	details, err := identity.Account(ctx, external.Token)
	if err != nil || details.HasPassword || !details.RadishLinked {
		t.Fatal("external account incorrectly depends on password", err)
	}
	if _, err := auth.ResolveWorkspace(ctx, external.Token, workspaceID); err != nil {
		t.Fatal("external session cannot resolve membership", err)
	}
	if err := identity.Unlink(ctx, external.Token); !errors.Is(err, authn.ErrLastLoginMethod) {
		t.Fatal("last login method removed", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, external.Account.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ResolveSession(ctx, external.Token); !errors.Is(err, authn.ErrInvalidSession) {
		t.Fatal("disabled external session accepted", err)
	}
	if _, err := auth.ResolveWorkspace(ctx, external.Token, workspaceID); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Fatal("disabled external membership accepted", err)
	}

	link, state := start(authn.OIDCStartInput{Mode: "link", SessionToken: member.Token})
	if _, linked, err := oidc.Callback(ctx, state, link.BrowserToken, "member-subject", false); err != nil || !linked {
		t.Fatal("explicit linking failed", err)
	}
	oidcLogin, state := start(authn.OIDCStartInput{Mode: "login"})
	signedIn, _, err := oidc.Callback(ctx, state, oidcLogin.BrowserToken, "member-subject", false)
	if err != nil || signedIn.Account.User.ID != member.Account.User.ID {
		t.Fatal("binding did not preserve user ID", err)
	}
	if err := identity.Unlink(ctx, member.Token); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{member.Token, signedIn.Token} {
		if _, err := auth.ResolveSession(ctx, token); !errors.Is(err, authn.ErrInvalidSession) {
			t.Fatal("unlink retained old session", err)
		}
	}

	// Revocation after start invalidates link even though the browser's state
	// and provider authentication are otherwise valid.
	relogin, err := auth.Login(ctx, authn.LoginInput{Email: "member@example.test", Password: "invited member password"})
	if err != nil {
		t.Fatal(err)
	}
	stale, state := start(authn.OIDCStartInput{Mode: "link", SessionToken: relogin.Token})
	if err := auth.RevokeSession(ctx, relogin.Token, relogin.CSRFToken); err != nil {
		t.Fatal(err)
	}
	if _, _, err := oidc.Callback(ctx, state, stale.BrowserToken, "stale-subject", false); !errors.Is(err, authn.ErrInvalidSession) {
		t.Fatal("revoked initiating session linked", err)
	}
	var leakedCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.user_accounts WHERE user_id IN (SELECT user_id FROM radishnexus.local_credentials WHERE email IN ('replay@example.test','revoked@example.test'))`).Scan(&leakedCount); err != nil || leakedCount != 0 {
		t.Fatal("failed admission left partial account", err)
	}
}
