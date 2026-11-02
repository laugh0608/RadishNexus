package httptransport

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func (h *CollaborationHandler) rejectDecision(w http.ResponseWriter, r *http.Request) {
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
	raw, err := decodeStrictObject(w, r, []string{"client_operation_id", "rejection_reason", "confirmed"}, 64*1024)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	input := goldenpath.RejectDecisionInput{DecisionID: id}
	var confirmed bool
	for key, target := range map[string]any{"client_operation_id": &input.ClientOperationID, "rejection_reason": &input.RejectionReason, "confirmed": &confirmed} {
		if key == "rejection_reason" {
			err = decodeUTF8String(raw[key], &input.RejectionReason)
		} else {
			err = json.Unmarshal(raw[key], target)
		}
		if !utf8.Valid(raw[key]) || string(raw[key]) == "null" || err != nil {
			h.writeError(w, r, authz.ErrInvalid)
			return
		}
	}
	if !confirmed || !goldenpath.ValidRejectionReason(input.RejectionReason) {
		h.writeError(w, r, authz.ErrInvalid)
		return
	}
	result, err := h.collaboration.RejectDecision(r.Context(), webInvocation(p, r), input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	d, err := publicDecision(result.Decision)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if result.Decision.WorkspaceID != workspace || result.Decision.ID != id || result.Decision.Status != "rejected" ||
		result.Decision.RejectionReason != strings.TrimSpace(input.RejectionReason) || len(result.Decision.DeciderIDs) != 1 || result.Decision.DeciderIDs[0] != p.ID {
		h.writeError(w, r, errors.New("invalid rejected Decision application result"))
		return
	}
	h.writeJSON(w, r, http.StatusOK, decisionResponse{Data: d})
}
