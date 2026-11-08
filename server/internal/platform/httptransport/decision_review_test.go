package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type reviewTestApp struct {
	fakeCollaborationApplication
	record goldenpath.DecisionReviewRecord
	page   goldenpath.DecisionReviewPageInput
	calls  int
}

func (app *reviewTestApp) ReviewDecision(_ context.Context, _ goldenpath.Invocation, _ goldenpath.ReviewDecisionInput) (goldenpath.DecisionReviewRecord, bool, error) {
	app.calls++
	return app.record, app.calls == 1, nil
}

func (app *reviewTestApp) ListDecisionReviews(_ context.Context, _ authz.Principal, _ string, input goldenpath.DecisionReviewPageInput) (goldenpath.DecisionReviewPage, error) {
	app.page = input
	if input.AfterID != "" {
		return goldenpath.DecisionReviewPage{}, nil
	}
	return goldenpath.DecisionReviewPage{Items: []goldenpath.DecisionReviewRecord{app.record}, HasMore: true}, nil
}

func TestDecisionReviewTransportStrictWriteAndScopedCursor(t *testing.T) {
	app := &reviewTestApp{record: goldenpath.DecisionReviewRecord{
		EventID: "evt_review", DecisionID: "dec_old", Actor: goldenpath.ActorRef{Kind: "user", ID: "usr_reader"},
		Facts:            cp.DecisionReview{DecisionRevision: 2, Trigger: "manual", Conclusion: "still_applicable", Reason: "New checks passed."},
		OccurredAt:       time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC),
		OriginalEvidence: []entityref.Ref{{Type: "thread", ID: "thr_origin"}},
	}}
	h := testCollaborationHandler(t, validMessagingSessions(), app)
	path := "/api/v1/workspaces/wrk_main/decisions/dec_old/reviews"
	valid := `{"client_operation_id":"web:review","base_decision_revision":2,"conclusion":"still_applicable","reason":" New checks passed. ","additional_evidence":null,"confirmed":true}`
	for _, body := range []string{
		strings.Replace(valid, `"confirmed":true`, `"confirmed":false`, 1),
		strings.Replace(valid, `" New checks passed. "`, `"\ud800"`, 1),
		strings.Replace(valid, `"additional_evidence":null`, `"additional_evidence":{"type":"thread","type":"thread","id":"thr_extra"}`, 1),
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, messagingWriteRequest(http.MethodPost, path, body))
		if w.Code != http.StatusBadRequest || app.calls != 0 {
			t.Fatalf("invalid review reached app, status %d", w.Code)
		}
	}
	for _, status := range []int{http.StatusCreated, http.StatusOK} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, messagingWriteRequest(http.MethodPost, path, valid))
		if w.Code != status || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("review status = %d, want %d", w.Code, status)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, messagingRequest(http.MethodGet, path+"?limit=1", ""))
	var response struct {
		Data struct {
			Next string `json:"next_after"`
		} `json:"data"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Data.Next == "" {
		t.Fatalf("review first page status %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, messagingRequest(http.MethodGet, path+"?after="+response.Data.Next, ""))
	if w.Code != http.StatusOK || app.page.AfterID != app.record.EventID || !app.page.AfterTime.Equal(app.record.OccurredAt) {
		t.Fatalf("review cursor status %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, messagingRequest(http.MethodGet, strings.Replace(path, "dec_old", "dec_other", 1)+"?after="+response.Data.Next, ""))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("review accepted cursor from another Decision, status %d", w.Code)
	}
}
