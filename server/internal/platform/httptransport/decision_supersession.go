package httptransport

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

type supersessionRelationDTO struct {
	Reason     string             `json:"reason"`
	Actor      deploymentActorDTO `json:"actor"`
	OccurredAt string             `json:"occurred_at"`
}
type supersessionTimelineDTO struct {
	Side         string `json:"side"`
	FromRevision int    `json:"from_revision"`
	ToRevision   int    `json:"to_revision"`
}
type supersessionResultDTO struct {
	Superseded              entityRefDTO `json:"superseded"`
	Replacement             entityRefDTO `json:"replacement"`
	Relation                entityRefDTO `json:"relation"`
	Reason                  string       `json:"supersession_reason"`
	FromRevision            int          `json:"from_revision"`
	ToRevision              int          `json:"to_revision"`
	ReplacementFromRevision int          `json:"replacement_from_revision"`
	ReplacementToRevision   int          `json:"replacement_to_revision"`
	OccurredAt              string       `json:"occurred_at"`
}

func (h *CollaborationHandler) supersedeDecision(w http.ResponseWriter, r *http.Request) {
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
	raw, err := decodeStrictObject(w, r, []string{"client_operation_id", "replacement_decision_id", "base_decision_revision", "base_replacement_revision", "supersession_reason", "confirmed"}, 65536)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	in := goldenpath.SupersedeDecisionInput{DecisionID: id}
	var confirmed bool
	for key, target := range map[string]any{"client_operation_id": &in.ClientOperationID, "replacement_decision_id": &in.ReplacementID, "base_decision_revision": &in.BaseDecisionRevision, "base_replacement_revision": &in.BaseReplacementRevision, "supersession_reason": &in.SupersessionReason, "confirmed": &confirmed} {
		if key == "supersession_reason" {
			err = decodeUTF8String(raw[key], &in.SupersessionReason)
		} else {
			err = json.Unmarshal(raw[key], target)
		}
		if err != nil || string(raw[key]) == "null" {
			h.writeError(w, r, authz.ErrInvalid)
			return
		}
	}
	if !confirmed || !goldenpath.ValidRejectionReason(in.SupersessionReason) {
		h.writeError(w, r, authz.ErrInvalid)
		return
	}
	result, err := h.collaboration.SupersedeDecision(r.Context(), webInvocation(p, r), in)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if result.DecisionID != id || result.ReplacementID != in.ReplacementID || result.SupersessionReason != strings.TrimSpace(in.SupersessionReason) || result.FromRevision != in.BaseDecisionRevision || result.ReplacementFromRevision != in.BaseReplacementRevision || (result.FromRevision != 2 && result.FromRevision != 3) || result.ReplacementFromRevision != 2 || result.ToRevision != result.FromRevision+1 || result.ReplacementToRevision != result.ReplacementFromRevision+1 || result.OccurredAt.IsZero() || !validScopedID(result.LinkID, "lnk_") {
		h.writeError(w, r, errors.New("invalid supersession application result"))
		return
	}
	data := supersessionResultDTO{Superseded: publicRef(entityref.Ref{Type: "decision", ID: id}), Replacement: publicRef(entityref.Ref{Type: "decision", ID: result.ReplacementID}), Relation: publicRef(entityref.Ref{Type: "entity-link", ID: result.LinkID}), Reason: result.SupersessionReason, FromRevision: result.FromRevision, ToRevision: result.ToRevision, ReplacementFromRevision: result.ReplacementFromRevision, ReplacementToRevision: result.ReplacementToRevision, OccurredAt: publicTime(result.OccurredAt)}
	h.writeJSON(w, r, 200, struct {
		Data supersessionResultDTO `json:"data"`
	}{data})
}
