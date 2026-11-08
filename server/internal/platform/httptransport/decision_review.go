package httptransport

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const decisionReviewsPattern = "/api/v1/workspaces/{workspace_id}/decisions/{decision_id}/reviews"

type decisionReviewDTO struct {
	EventID            string             `json:"event_id"`
	Decision           entityRefDTO       `json:"decision"`
	DecisionRevision   int                `json:"decision_revision"`
	Trigger            string             `json:"trigger"`
	Conclusion         string             `json:"conclusion"`
	Reason             string             `json:"reason"`
	Actor              deploymentActorDTO `json:"actor"`
	OccurredAt         string             `json:"occurred_at"`
	OriginalEvidence   []entityRefDTO     `json:"original_evidence"`
	AdditionalEvidence *entityRefDTO      `json:"additional_evidence"`
	Imported           bool               `json:"imported"`
}

type decisionReviewCursor struct {
	Workspace string    `json:"workspace"`
	Decision  string    `json:"decision"`
	Actor     string    `json:"actor"`
	At        time.Time `json:"at"`
	Event     string    `json:"event"`
}

func (h *CollaborationHandler) reviewDecision(w http.ResponseWriter, r *http.Request) {
	workspace, id := r.PathValue("workspace_id"), r.PathValue("decision_id")
	p, err := h.authenticate(r, workspace, true)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if _, err = validateCollaborationPath(workspace, "decision", id); err != nil {
		h.writeError(w, r, err)
		return
	}
	if r.URL.RawQuery != "" {
		h.writeError(w, r, authz.ErrInvalid)
		return
	}
	raw, err := decodeStrictObject(w, r, []string{"client_operation_id", "base_decision_revision", "conclusion", "reason", "additional_evidence", "confirmed"}, 64*1024)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	input := goldenpath.ReviewDecisionInput{DecisionID: id}
	var confirmed bool
	if json.Unmarshal(raw["confirmed"], &confirmed) != nil || !confirmed ||
		json.Unmarshal(raw["client_operation_id"], &input.ClientOperationID) != nil ||
		json.Unmarshal(raw["base_decision_revision"], &input.BaseDecisionRevision) != nil ||
		json.Unmarshal(raw["conclusion"], &input.Conclusion) != nil ||
		decodeUTF8String(raw["reason"], &input.Reason) != nil {
		h.writeError(w, r, authz.ErrInvalid)
		return
	}
	var ref *cp.Ref
	if cp.Decode(raw["additional_evidence"], &ref) != nil {
		h.writeError(w, r, authz.ErrInvalid)
		return
	}
	if ref != nil {
		if _, err := decodeStrictObjectBytes(raw["additional_evidence"], []string{"type", "id"}); err != nil {
			h.writeError(w, r, err)
			return
		}
		input.AdditionalEvidence = &entityref.Ref{Type: ref.Type, ID: ref.ID}
	}
	record, applied, err := h.collaboration.ReviewDecision(r.Context(), webInvocation(p, r), input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	data, err := publicDecisionReview(record, id)
	if err != nil || record.Imported || record.Actor.ID != p.ID || record.Facts.DecisionRevision != input.BaseDecisionRevision ||
		record.Facts.Conclusion != input.Conclusion || record.Facts.Reason != strings.TrimSpace(input.Reason) ||
		(record.Facts.AdditionalEvidence == nil) != (ref == nil) || (ref != nil && *record.Facts.AdditionalEvidence != *ref) {
		h.writeError(w, r, errors.New("invalid Decision review application result"))
		return
	}
	status := http.StatusOK
	if applied {
		status = http.StatusCreated
	}
	h.writeJSON(w, r, status, struct {
		Data decisionReviewDTO `json:"data"`
	}{data})
}

func (h *CollaborationHandler) getDecisionReviews(w http.ResponseWriter, r *http.Request) {
	workspace, id := r.PathValue("workspace_id"), r.PathValue("decision_id")
	p, err := h.authenticate(r, workspace, false)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if _, err = validateCollaborationPath(workspace, "decision", id); err != nil {
		h.writeError(w, r, err)
		return
	}
	scope := decisionReviewCursor{Workspace: workspace, Decision: id, Actor: p.ID}
	input, err := parseDecisionReviewQuery(r.URL.RawQuery, scope)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	page, err := h.collaboration.ListDecisionReviews(r.Context(), p, id, input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if len(page.Items) > input.Limit || (page.HasMore && len(page.Items) != input.Limit) {
		h.writeError(w, r, errors.New("invalid Decision review page"))
		return
	}
	items := make([]decisionReviewDTO, 0, len(page.Items))
	for _, review := range page.Items {
		dto, err := publicDecisionReview(review, id)
		if err != nil {
			h.writeError(w, r, err)
			return
		}
		items = append(items, dto)
	}
	var next *string
	if page.HasMore {
		last := page.Items[len(page.Items)-1]
		scope.At, scope.Event = last.OccurredAt.UTC(), last.EventID
		token := base64.RawURLEncoding.EncodeToString(cp.Encode(scope))
		next = &token
	}
	data := struct {
		Items     []decisionReviewDTO `json:"items"`
		NextAfter *string             `json:"next_after"`
	}{items, next}
	h.writeJSON(w, r, http.StatusOK, struct {
		Data any `json:"data"`
	}{data})
}

func parseDecisionReviewQuery(raw string, scope decisionReviewCursor) (goldenpath.DecisionReviewPageInput, error) {
	input := goldenpath.DecisionReviewPageInput{Limit: 20}
	if len(raw) > 2048 {
		return input, authz.ErrInvalid
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return input, authz.ErrInvalid
	}
	for name, entries := range values {
		if (name != "limit" && name != "after") || len(entries) != 1 || entries[0] == "" {
			return input, authz.ErrInvalid
		}
	}
	if value := values.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || strconv.Itoa(limit) != value || limit < 1 || limit > goldenpath.MaxDecisionReviewPageSize {
			return input, authz.ErrInvalid
		}
		input.Limit = limit
	}
	if token := values.Get("after"); token != "" {
		body, err := base64.RawURLEncoding.Strict().DecodeString(token)
		var cursor decisionReviewCursor
		if err != nil || cp.Decode(body, &cursor) != nil ||
			base64.RawURLEncoding.EncodeToString(cp.Encode(cursor)) != token || cursor.At.IsZero() ||
			cursor.At.Location() != time.UTC || !cp.ValidID(cursor.Event, "evt_") {
			return input, authz.ErrInvalid
		}
		input.AfterTime, input.AfterID = cursor.At, cursor.Event
		cursor.At, cursor.Event = time.Time{}, ""
		if cursor != scope {
			return input, authz.ErrInvalid
		}
	}
	return input, nil
}

func publicDecisionReview(record goldenpath.DecisionReviewRecord, id string) (decisionReviewDTO, error) {
	facts, err := cp.DecodeDecisionReview(cp.Encode(record.Facts))
	if err != nil || record.DecisionID != id || !cp.ValidID(record.EventID, "evt_") ||
		record.Actor.Kind != "user" || !cp.ValidID(record.Actor.ID, "usr_") ||
		record.OccurredAt.IsZero() || len(record.OriginalEvidence) == 0 {
		return decisionReviewDTO{}, errors.New("invalid public Decision review")
	}
	evidence := make([]entityRefDTO, 0, len(record.OriginalEvidence))
	for _, ref := range record.OriginalEvidence {
		if ref.Type != "thread" || !cp.ValidID(ref.ID, "thr_") ||
			(facts.AdditionalEvidence != nil && ref.ID == facts.AdditionalEvidence.ID) {
			return decisionReviewDTO{}, errors.New("invalid public Decision review evidence")
		}
		evidence = append(evidence, publicRef(ref))
	}
	var extra *entityRefDTO
	if ref := facts.AdditionalEvidence; ref != nil {
		dto := publicRef(entityref.Ref{Type: ref.Type, ID: ref.ID})
		extra = &dto
	}
	return decisionReviewDTO{
		EventID: record.EventID, Decision: publicRef(entityref.Ref{Type: "decision", ID: id}),
		DecisionRevision: facts.DecisionRevision, Trigger: facts.Trigger, Conclusion: facts.Conclusion,
		Reason: facts.Reason, Actor: deploymentActorDTO{Kind: record.Actor.Kind, ID: record.Actor.ID},
		OccurredAt: publicTime(record.OccurredAt), OriginalEvidence: evidence, AdditionalEvidence: extra,
		Imported: record.Imported,
	}, nil
}
