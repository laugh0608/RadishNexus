package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

func TestDecisionRejectionTransportAndProjection(t *testing.T) {
	now := time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC)
	d := goldenpath.Decision{DecisionRevision: 2, ID: "dec_target", WorkspaceID: "wrk_main", GoverningProjectID: "prj_main", Question: "Choose?", Status: "rejected", RejectionReason: "Reason", ProposerID: "usr_writer", DeciderIDs: []string{"usr_reader"}, DecidedAt: &now, CreatedAt: now.Add(-time.Second), UpdatedAt: now}
	a := &fakeCollaborationApplication{rejectionResult: goldenpath.RejectDecisionResult{Decision: d, Rejected: true}}
	h := testCollaborationHandler(t, validMessagingSessions(), a)
	path := "/api/v1/workspaces/wrk_main/decisions/dec_target/rejection"
	valid := `{"client_operation_id":"web:reject","rejection_reason":" Reason ","confirmed":true}`
	for _, body := range []string{
		`{}`, `null`, `{"client_operation_id":"web:reject","rejection_reason":null,"confirmed":true}`,
		`{"client_operation_id":"web:reject","confirmed":true}`,
		`{"client_operation_id":"web:reject","rejection_reason":"Reason","confirmed":null}`,
		`{"client_operation_id":"web:reject","rejection_reason":" \n ","confirmed":true}`,
		`{"client_operation_id":"web:reject","rejection_reason":"bad\u0000","confirmed":true}`,
		`{"client_operation_id":"web:reject","rejection_reason":"` + strings.Repeat("x", 32769) + `","confirmed":true}`,
		`{"client_operation_id":"web:reject","rejection_reason":"Reason","confirmed":false}`,
		`{"client_operation_id":"web:reject","rejection_reason":"Reason","confirmed":true,"confirmed":true}`,
		`{"client_operation_id":"web:reject","rejection_reason":"Reason","confirmed":true,"actor":"usr_other"}`,
		`{"client_operation_id":"web:reject","rejection_reason":"Reason \ud800","confirmed":true}`,
		`{"client_operation_id":"web:reject","rejection_reason":"Reason \udc00","confirmed":true}`,
		"{\"client_operation_id\":\"web:reject\",\"rejection_reason\":\"" + string([]byte{0xff}) + "\",\"confirmed\":true}",
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, messagingWriteRequest(http.MethodPost, path, body))
		if w.Code != 400 || a.rejectionCalls != 0 {
			t.Fatal("invalid body reached command", w.Code)
		}
	}
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Header.Del("Origin") },
		func(r *http.Request) { r.Header.Set("Origin", "https://other.example.test") },
		func(r *http.Request) { r.Header.Del(CSRFHeaderName) },
	} {
		r := messagingWriteRequest(http.MethodPost, path, valid)
		mutate(r)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 || a.rejectionCalls != 0 || w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("Vary") != "Cookie" {
			t.Fatal("invalid write authority or cache policy", w.Code)
		}
	}
	oversized := httptest.NewRecorder()
	h.ServeHTTP(oversized, messagingWriteRequest(http.MethodPost, path, strings.Repeat(" ", 65537)))
	if oversized.Code != 413 || a.rejectionCalls != 0 {
		t.Fatal("body size bound", oversized.Code)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, messagingWriteRequest(http.MethodPost, path, valid))
	if w.Code != 200 || a.rejectionCalls != 1 || a.rejectionInput.RejectionReason != " Reason " || !strings.Contains(w.Body.String(), `"rejection_reason":"Reason"`) {
		t.Fatal("rejection", w.Code, w.Body.String())
	}
	for _, forbidden := range []string{"client_operation_id", "payload_sha256", "receipt", "Reason \""} {
		if strings.Contains(w.Body.String(), forbidden) {
			t.Fatal("response leak", forbidden)
		}
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, messagingWriteRequest(http.MethodPost, path+"?extra=1", valid))
	if w.Code != 400 {
		t.Fatal("query accepted", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, messagingRequest(http.MethodGet, path, ""))
	if w.Code != 405 || w.Header().Get("Allow") != "POST" {
		t.Fatal("method", w.Code, w.Header())
	}
	view := goldenpath.NexusView{Current: goldenpath.CurrentProjection{DecisionRevision: d.DecisionRevision, Ref: entityref.Ref{Type: "decision", ID: d.ID}, GoverningProjectID: d.GoverningProjectID, Title: d.Question, Status: d.Status, RejectionReason: d.RejectionReason, ProposerID: d.ProposerID, DeciderIDs: d.DeciderIDs, DecidedAt: d.DecidedAt, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt}, DecisionCapabilities: &goldenpath.DecisionCapabilities{}, Relations: []goldenpath.RelationProjection{{State: goldenpath.ProjectionRestricted}}, Timeline: []goldenpath.TimelineItem{{EventID: "evt_rejected", ActivityType: "decision.rejected", Actor: goldenpath.ActorRef{Kind: "user", ID: "usr_reader"}, OccurredAt: now, ProjectionVersion: goldenpath.ActivityProjectionVersion, SafeFacts: map[string]string{"status": "rejected"}}}}
	if _, e := publicCollaborationView(view.Current.Ref, view); e != nil {
		t.Fatal("rejected view", e)
	}
	for _, mutate := range []func(*goldenpath.NexusView){func(v *goldenpath.NexusView) { v.DecisionCapabilities = nil }, func(v *goldenpath.NexusView) {
		v.DecisionCapabilities = &goldenpath.DecisionCapabilities{Accept: true, Reject: true}
	}, func(v *goldenpath.NexusView) { v.Current.RejectionReason = "" }, func(v *goldenpath.NexusView) { v.Current.Outcome = "invalid" }} {
		bad := view
		mutate(&bad)
		if _, e := publicCollaborationView(bad.Current.Ref, bad); e == nil {
			t.Fatal("invalid rejected projection accepted")
		}
	}
}
