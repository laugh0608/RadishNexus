package httptransport

import (
	"context"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type managementTestSessions struct{ *fakeMessagingSessions }

func (s managementTestSessions) ResolveSession(context.Context, string) (authn.SessionAccount, error) {
	return testSessionAccount(time.Now().Add(time.Hour)), nil
}

type managementTestApplication struct {
	ManagementApplication
	calls int
	p     authz.Principal
	input goldenpath.ManagementInput
	out   goldenpath.ManagementResult
}

func (a *managementTestApplication) Apply(_ context.Context, v goldenpath.Invocation, _ string, in goldenpath.ManagementInput) (goldenpath.ManagementResult, error) {
	a.calls++
	a.p = v.Principal
	a.input = in
	return a.out, nil
}
func (a *managementTestApplication) Result(_ context.Context, p authz.Principal, _, _ string) (goldenpath.ManagementResult, error) {
	a.calls++
	a.p = p
	return a.out, nil
}
func (a *managementTestApplication) Preview(_ context.Context, p authz.Principal, _ string, in goldenpath.ManagementProposal) (goldenpath.ManagementPlan, error) {
	a.calls++
	out := goldenpath.ManagementPlan{ManagementProposal: in, ActorID: p.ID, Policy: "membership-management-v1"}
	out.Digest, _ = out.SHA256(p.WorkspaceID)
	return out, nil
}
func TestManagementStrictHTTPAndGlobalReceipt(t *testing.T) {
	now := time.Date(2026, 10, 24, 18, 0, 0, 0, time.FixedZone("test-local", 8*60*60))
	a := &managementTestApplication{out: goldenpath.ManagementResult{Result: "applied", WorkspaceID: "wrk_main", ClientOperationID: ownerTestOperation, Action: "management.project.role", ScopeID: "prj_main", TargetUserID: "usr_target", OccurredAt: &now}}
	sessions := managementTestSessions{validMessagingSessions()}
	policy, _ := NewBrowserSessionPolicy("https://nexus.example.test")
	proxy, _ := NewTrustedProxyPolicy("10.0.0.0/8")
	h := WithRequestID(NewManagementHandler(sessions, a, policy, proxy))
	invoke := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, messagingWriteRequest(method, path, body))
		return r
	}
	path := "/api/v1/workspaces/wrk_main/projects/prj_main/management/role"
	body := `{"target_user_id":"usr_target","role":"admin","actor_after_role":"","client_operation_id":"` + ownerTestOperation + `","plan_digest":"` + strings.Repeat("a", 64) + `","confirmed":true}`
	r := invoke("POST", path, body)
	if r.Code != 200 || a.calls != 1 || a.p.ID != "usr_reader" || a.input.ScopeID != "prj_main" {
		t.Fatal(r.Code, r.Body.String())
	}
	if !strings.Contains(r.Body.String(), `"occurred_at":"2026-10-24T10:00:00Z"`) {
		t.Fatal("non-UTC public time", r.Body.String())
	}
	if r.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("cacheable management")
	}
	for _, bad := range []string{`null`, `[]`, body + `{}`, strings.Replace(body, `"confirmed":true`, `"confirmed":false`, 1), strings.Replace(body, `"confirmed":true`, `"confirmed":null`, 1), strings.Replace(body, `"role":"admin"`, `"role":"owner"`, 1), strings.Replace(body, `"confirmed":true`, `"confirmed":true,"confirmed":true`, 1), strings.Replace(body, `"confirmed":true`, `"confirmed":true,"actor_id":"usr_other"`, 1)} {
		r = invoke("POST", path, bad)
		if r.Code != 400 || a.calls != 1 {
			t.Fatal("invalid reached application", r.Code, r.Body.String())
		}
	}
	for _, change := range []func(*http.Request){func(r *http.Request) { r.Header.Del("Cookie") }, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.example.test") }} {
		req := messagingWriteRequest("POST", path, body)
		change(req)
		r := httptest.NewRecorder()
		h.ServeHTTP(r, req)
		if r.Code < 400 || a.calls != 1 {
			t.Fatal("unauthenticated change", r.Code)
		}
	}
	// Workspace resolution may fail after a real exit; the history endpoint must
	// use only the global session, never an old Workspace admission.
	sessions.resolveErr = authz.ErrNotFound
	r = invoke("GET", "/api/v1/auth/management-results/wrk_main/"+ownerTestOperation, "")
	if r.Code != 200 || a.calls != 2 {
		t.Fatal("receipt required membership", r.Code, r.Body.String())
	}
	a.out.Invitation = &goldenpath.ManagementInvitation{Token: strings.Repeat("a", 43), ExpiresAt: now.Add(time.Hour)}
	r = invoke("GET", "/api/v1/auth/management-results/wrk_main/"+ownerTestOperation, "")
	if r.Code != 500 || strings.Contains(r.Body.String(), a.out.Invitation.Token) {
		t.Fatal("history leaked credential", r.Code)
	}
}
