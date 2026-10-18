package httptransport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type ownerTestApplication struct {
	calls  int
	input  goldenpath.OwnerTransferInput
	token  string
	p      authz.Principal
	err    error
	result goldenpath.OwnerTransferResult
	page   goldenpath.OwnerCandidatePage
	after  string
}

func (a *ownerTestApplication) Ownership(_ context.Context, p authz.Principal, token string) (goldenpath.Ownership, error) {
	a.calls++
	a.p = p
	a.token = token
	return goldenpath.Ownership{UserID: p.ID, Role: "owner", RoleRevision: 9007199254740993, RecentlyAuthenticated: true}, a.err
}
func (a *ownerTestApplication) Candidates(_ context.Context, p authz.Principal, token, after string, _ int) (goldenpath.OwnerCandidatePage, error) {
	a.calls++
	a.p = p
	a.token = token
	a.after = after
	return a.page, a.err
}
func (a *ownerTestApplication) Transfer(_ context.Context, i goldenpath.Invocation, token string, in goldenpath.OwnerTransferInput) (goldenpath.OwnerTransferResult, error) {
	a.calls++
	a.p = i.Principal
	a.token = token
	a.input = in
	return a.result, a.err
}
func (a *ownerTestApplication) Status(_ context.Context, p authz.Principal, token, _ string) (goldenpath.OwnerTransferResult, error) {
	a.calls++
	a.p = p
	a.token = token
	return a.result, a.err
}
func ownerTestHandler(t *testing.T, a *ownerTestApplication) http.Handler {
	t.Helper()
	policy, e := NewBrowserSessionPolicy("https://nexus.example.test")
	if e != nil {
		t.Fatal(e)
	}
	proxy, e := NewTrustedProxyPolicy("10.0.0.0/8")
	if e != nil {
		t.Fatal(e)
	}
	return WithRequestID(NewOwnerTransferHandler(validMessagingSessions(), a, policy, proxy))
}

const ownerTestOperation = "12345678-1234-4234-8234-123456789abc"
const ownerTestBody = `{"client_operation_id":"12345678-1234-4234-8234-123456789abc","target_user_id":"usr_target","expected_actor_role_revision":"9007199254740993","expected_target_role_revision":"1","confirmed":true}`

func ownerTestResult() goldenpath.OwnerTransferResult {
	return goldenpath.OwnerTransferResult{Result: "applied", WorkspaceID: "wrk_main", ClientOperationID: ownerTestOperation, ActorUserID: "usr_reader", TargetUserID: "usr_target", AuditID: "cfa_transfer", OccurredAt: time.Now().UTC(), ActorRoleRevision: 9007199254740994, TargetRoleRevision: 2}
}
func TestOwnerTransferStrictHTTPAndSession(t *testing.T) {
	a := &ownerTestApplication{result: ownerTestResult()}
	h := ownerTestHandler(t, a)
	path := "/api/v1/workspaces/wrk_main/owner-transfers"
	invoke := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, messagingWriteRequest(method, path, body))
		return r
	}
	r := invoke("POST", path, ownerTestBody)
	if r.Code != 200 || a.calls != 1 || a.p.ID != "usr_reader" || a.token == "" || !strings.Contains(r.Body.String(), `"actor_role_revision":"9007199254740994"`) {
		t.Fatal(r.Code, r.Body.String())
	}
	if r.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("history cacheable")
	}
	for _, bad := range []string{`null`, `[]`, ownerTestBody + `{}`, strings.Replace(ownerTestBody, `"confirmed":true`, `"confirmed":false`, 1), strings.Replace(ownerTestBody, `"confirmed":true`, `"confirmed":null`, 1), strings.Replace(ownerTestBody, `,"confirmed":true`, "", 1), strings.Replace(ownerTestBody, `"confirmed":true`, `"confirmed":true,"confirmed":true`, 1), strings.Replace(ownerTestBody, `"confirmed":true`, `"confirmed":true,"actor_id":"usr_admin"`, 1), strings.Replace(ownerTestBody, `"1"`, `1`, 1), strings.Replace(ownerTestBody, `"1"`, `"01"`, 1), strings.Replace(ownerTestBody, `"1"`, `"9223372036854775808"`, 1), strings.Replace(ownerTestBody, "usr_target", "usr_reader", 1), strings.Replace(ownerTestBody, "4234", "5234", 1), strings.Replace(ownerTestBody, "usr_target", "wrk_wrong", 1)} {
		r = invoke("POST", path, bad)
		if r.Code != 400 || a.calls != 1 {
			t.Fatal("invalid body reached application", r.Code, r.Body.String())
		}
	}
	r = invoke("POST", path, strings.Repeat(" ", 8193)+ownerTestBody)
	if r.Code != 413 || a.calls != 1 {
		t.Fatal("unbounded request", r.Code)
	}
	for _, change := range []func(*http.Request){func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.example.test") }, func(r *http.Request) { r.Header.Del("Cookie") }} {
		req := messagingWriteRequest("POST", path, ownerTestBody)
		change(req)
		r = httptest.NewRecorder()
		h.ServeHTTP(r, req)
		if r.Code < 400 || a.calls != 1 {
			t.Fatal("authentication bypass", r.Code)
		}
	}
	r = invoke("POST", path+"?confirmed=true", ownerTestBody)
	if r.Code != 400 {
		t.Fatal(r.Code)
	}
	r = invoke("GET", path, "")
	if r.Code != 405 || r.Header().Get("Allow") != "POST" {
		t.Fatal(r.Code)
	}
}
func TestOwnerTransferHTTPHistoryAndErrorBoundaries(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{{authn.ErrInvalidSession, 401, "unauthenticated"}, {authz.ErrForbidden, 403, "forbidden"}, {authn.ErrRecentAuthentication, 403, "recent_authentication_required"}, {authz.ErrNotFound, 404, "not_found"}, {authz.ErrConflict, 409, "conflict"}, {goldenpath.ErrOwnerTransferUnconfirmed, 503, "result_unconfirmed"}, {errors.New("secret internal query"), 500, "internal"}} {
		a := &ownerTestApplication{err: test.err}
		r := httptest.NewRecorder()
		ownerTestHandler(t, a).ServeHTTP(r, messagingWriteRequest("POST", "/api/v1/workspaces/wrk_main/owner-transfers", ownerTestBody))
		if r.Code != test.status || !strings.Contains(r.Body.String(), `"code":"`+test.code+`"`) || strings.Contains(r.Body.String(), "secret") {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	a := &ownerTestApplication{result: ownerTestResult()}
	h := ownerTestHandler(t, a)
	path := "/api/v1/workspaces/wrk_main/owner-transfers/" + ownerTestOperation
	r := httptest.NewRecorder()
	h.ServeHTTP(r, messagingRequest("GET", path, ""))
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, change := range []func(*goldenpath.OwnerTransferResult){func(o *goldenpath.OwnerTransferResult) { o.ActorUserID = "usr_other" }, func(o *goldenpath.OwnerTransferResult) { o.WorkspaceID = "wrk_other" }, func(o *goldenpath.OwnerTransferResult) { o.ClientOperationID = "different" }, func(o *goldenpath.OwnerTransferResult) { o.TargetUserID = o.ActorUserID }, func(o *goldenpath.OwnerTransferResult) { o.Result = "not_observed" }} {
		a.result = ownerTestResult()
		change(&a.result)
		r = httptest.NewRecorder()
		h.ServeHTTP(r, messagingRequest("GET", path, ""))
		if r.Code != 500 || strings.Contains(r.Body.String(), "usr_other") {
			t.Fatal("projection drift leaked", r.Code)
		}
	}
	a.result = goldenpath.OwnerTransferResult{Result: "not_observed", WorkspaceID: "wrk_main", ClientOperationID: ownerTestOperation}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, messagingRequest("GET", path, ""))
	if r.Code != 200 || strings.Contains(r.Body.String(), "actor_user_id") {
		t.Fatal("not_observed shape", r.Code, r.Body.String())
	}
}
func TestOwnerCandidatesCursorBoundToActorAndScope(t *testing.T) {
	a := &ownerTestApplication{page: goldenpath.OwnerCandidatePage{Items: []goldenpath.OwnerCandidate{{ID: "usr_a", DisplayName: "Same Name", RoleRevision: 1}}, NextID: "usr_a"}}
	h := ownerTestHandler(t, a)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, messagingRequest("GET", "/api/v1/workspaces/wrk_main/owner-transfer-candidates?limit=1", ""))
	if r.Code != 200 || strings.Contains(r.Body.String(), "email") || strings.Contains(r.Body.String(), "total") {
		t.Fatal(r.Code, r.Body.String())
	}
	var envelope struct {
		Data struct {
			NextCursor string `json:"next_cursor"`
		} `json:"data"`
	}
	if e := json.Unmarshal(r.Body.Bytes(), &envelope); e != nil {
		t.Fatal(e)
	}
	raw, e := base64.RawURLEncoding.DecodeString(envelope.Data.NextCursor)
	if e != nil {
		t.Fatal(e)
	}
	var cursor discoveryCursor
	if e = json.Unmarshal(raw, &cursor); e != nil {
		t.Fatal(e)
	}
	if cursor.ActorID != "usr_reader" || cursor.Kind != "owner-transfer-candidates" {
		t.Fatal(cursor)
	}
	for _, field := range []string{"actor", "workspace", "kind"} {
		bad := cursor
		switch field {
		case "actor":
			bad.ActorID = "usr_other"
		case "workspace":
			bad.WorkspaceID = "wrk_other"
		case "kind":
			bad.Kind = "workspace-members"
		}
		raw, _ = json.Marshal(bad)
		r = httptest.NewRecorder()
		h.ServeHTTP(r, messagingRequest("GET", "/api/v1/workspaces/wrk_main/owner-transfer-candidates?cursor="+base64.RawURLEncoding.EncodeToString(raw), ""))
		if r.Code != 400 || a.calls != 1 {
			t.Fatal("foreign cursor accepted", r.Code)
		}
	}
}
