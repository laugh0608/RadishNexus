package httptransport

import (
	"context"
	"encoding/json"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type assignmentApplication struct {
	calls  int
	in     goldenpath.TicketAssignmentInput
	inv    goldenpath.Invocation
	result goldenpath.TicketAssignmentResult
	page   goldenpath.AssigneeCandidates
	read   goldenpath.TicketAssignment
	err    error
}

func (s *assignmentApplication) SetTicketAssignee(_ context.Context, inv goldenpath.Invocation, in goldenpath.TicketAssignmentInput) (goldenpath.TicketAssignmentResult, error) {
	s.calls++
	s.in = in
	s.inv = inv
	return s.result, s.err
}
func (s *assignmentApplication) ReadTicketAssignment(context.Context, authz.Principal, string) (goldenpath.TicketAssignment, error) {
	s.calls++
	return s.read, s.err
}
func (s *assignmentApplication) ListAssigneeCandidates(context.Context, authz.Principal, string, goldenpath.DiscoveryPageInput) (goldenpath.AssigneeCandidates, error) {
	s.calls++
	return s.page, s.err
}
func TestTicketAssignmentHTTPBoundary(t *testing.T) {
	target := "usr_candidate"
	app := &assignmentApplication{result: goldenpath.TicketAssignmentResult{Applied: true, AssignmentRevision: 2, AssigneeUserID: &target}, read: goldenpath.TicketAssignment{AssignmentRevision: 1, StatusRevision: 1, CanAssign: true, ProjectManagementRevision: "1"}, page: goldenpath.AssigneeCandidates{Items: []goldenpath.AssigneeCandidate{{ID: target, DisplayName: "Candidate", LifecycleRevision: "1"}}, ProjectManagementRevision: "1", SelfID: "usr_reader", NextID: target}}
	policy, _ := NewBrowserSessionPolicy("https://nexus.example.test")
	proxy, _ := NewTrustedProxyPolicy("10.0.0.0/8")
	h := WithRequestID(NewTicketAssignmentHandler(validMessagingSessions(), app, policy, proxy))
	path := "/api/v1/workspaces/wrk_main/tickets/tkt_one/assignee"
	body := `{"client_operation_id":"assign","base_assignment_revision":1,"base_status_revision":1,"assignee_user_id":"usr_candidate","target_lifecycle_revision":"1","project_management_revision":"1","confirmed":true}`
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, messagingWriteRequest(method, path, body))
		return w
	}
	w := request("POST", path, body)
	if w.Code != 200 || app.calls != 1 || app.inv.Principal.ID != "usr_reader" || app.inv.SourceKind != "web" || app.in.TicketID != "tkt_one" || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, bad := range []string{strings.Replace(body, `"assignee_user_id":"usr_candidate",`, "", 1), strings.Replace(body, `"confirmed":true`, `"confirmed":false`, 1), strings.Replace(body, `"base_assignment_revision":1`, `"base_assignment_revision":null`, 1), strings.Replace(body, `"base_assignment_revision":1`, `"base_assignment_revision":1.5`, 1), strings.Replace(body, `"target_lifecycle_revision":"1"`, `"target_lifecycle_revision":"01"`, 1), strings.Replace(body, `"assignee_user_id":"usr_candidate"`, `"assignee_user_id":null`, 1), strings.Replace(body, `"confirmed":true`, `"confirmed":true,"actor":"usr_admin"`, 1), strings.Replace(body, `"confirmed":true`, `"confirmed":true,"confirmed":true`, 1), body + body} {
		w = request("POST", path, bad)
		if w.Code != 400 || app.calls != 1 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, func(r *http.Request) { r.Header.Set("Origin", "https://other.example.test") }, func(r *http.Request) { r.Header.Del("Cookie") }} {
		r := messagingWriteRequest("POST", path, body)
		mutate(r)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code < 400 || app.calls != 1 {
			t.Fatal(w.Code)
		}
	}
	if w = request("GET", path, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"assignee":null`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("GET", path+"/candidates?limit=1", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Data struct {
			Next string `json:"next_cursor"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w = request("GET", strings.Replace(path, "tkt_one", "tkt_other", 1)+"/candidates?after="+response.Data.Next, ""); w.Code != 400 {
		t.Fatal("cursor crossed ticket", w.Code)
	}
	if w = request("POST", path+"/candidates", body); w.Code != 405 {
		t.Fatal(w.Code)
	}
	app.result.AssignmentRevision = 3
	if w = request("POST", path, body); w.Code != 500 {
		t.Fatal("receipt mismatch accepted", w.Code)
	}
	app.read.Assignee = &goldenpath.TicketAssignee{ID: target, DisplayName: "Imported", Imported: true, Eligible: true}
	if w = request("GET", path, ""); w.Code != 500 {
		t.Fatal("invalid identity accepted", w.Code)
	}
}
