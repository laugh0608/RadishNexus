package goldenpath

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

const TicketAssigneeCommandKind = "ticket.assignee.set"

type TicketAssignee struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Eligible    bool   `json:"eligible"`
	Imported    bool   `json:"imported"`
}
type TicketAssignment struct {
	Assignee                  *TicketAssignee `json:"assignee"`
	AssignmentRevision        int             `json:"assignment_revision"`
	StatusRevision            int             `json:"status_revision"`
	CanAssign                 bool            `json:"can_assign"`
	ProjectManagementRevision string          `json:"project_management_revision"`
}
type AssigneeCandidate struct {
	ID                string `json:"id"`
	DisplayName       string `json:"display_name"`
	LifecycleRevision string `json:"lifecycle_revision"`
}
type AssigneeCandidates struct {
	Items                     []AssigneeCandidate `json:"items"`
	NextID                    string              `json:"-"`
	ProjectManagementRevision string              `json:"project_management_revision"`
	SelfID                    string              `json:"self_id"`
}
type TicketAssignmentInput struct {
	TicketID                  string  `json:"-"`
	ClientOperationID         string  `json:"client_operation_id"`
	BaseAssignmentRevision    int     `json:"base_assignment_revision"`
	BaseStatusRevision        int     `json:"base_status_revision"`
	AssigneeUserID            *string `json:"assignee_user_id"`
	TargetLifecycleRevision   string  `json:"target_lifecycle_revision"`
	ProjectManagementRevision string  `json:"project_management_revision"`
	Confirmed                 bool    `json:"confirmed"`
}
type TicketAssignmentChange struct {
	FromUserID   *string `json:"from_user_id"`
	ToUserID     *string `json:"to_user_id"`
	FromRevision int     `json:"from_revision"`
	ToRevision   int     `json:"to_revision"`
}

func SameAssignee(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func (v TicketAssignmentChange) Valid() bool {
	return !SameAssignee(v.FromUserID, v.ToUserID) && ValidTicketStatusRevision(v.FromRevision) && v.FromRevision < MaxTicketStatusRevision && v.ToRevision == v.FromRevision+1 && (v.FromUserID == nil || ValidConfigurationID(*v.FromUserID, "usr_")) && (v.ToUserID == nil || ValidConfigurationID(*v.ToUserID, "usr_"))
}
func DecodeTicketAssignment(payload []byte) (TicketAssignmentChange, error) {
	var v struct {
		Assignment TicketAssignmentChange `json:"assignment"`
	}
	var raw, fields map[string]json.RawMessage
	if json.Unmarshal(payload, &raw) != nil || len(raw) != 1 || json.Unmarshal(raw["assignment"], &fields) != nil || len(fields) != 4 || fields["from_user_id"] == nil || fields["to_user_id"] == nil || fields["from_revision"] == nil || fields["to_revision"] == nil || json.Unmarshal(payload, &v) != nil || !v.Assignment.Valid() {
		return v.Assignment, errors.New("invalid Ticket assignment event")
	}
	return v.Assignment, nil
}

type TicketAssignmentCommand struct {
	Invocation
	TicketAssignmentInput
	EventID, PayloadSHA256 string
}
type TicketAssignmentResult struct {
	Applied            bool    `json:"applied"`
	AssignmentRevision int     `json:"applied_assignment_revision"`
	AssigneeUserID     *string `json:"applied_assignee_user_id"`
}
type TicketAssignmentStore interface {
	ReadTicketAssignment(context.Context, authz.Principal, string) (TicketAssignment, error)
	ListAssigneeCandidates(context.Context, authz.Principal, string, DiscoveryPageInput) (AssigneeCandidates, error)
	SetTicketAssignee(context.Context, TicketAssignmentCommand) (TicketAssignmentResult, error)
}
type TicketAssignmentService struct {
	store TicketAssignmentStore
	ids   IDGenerator
}

func NewTicketAssignmentService(s TicketAssignmentStore, ids IDGenerator) *TicketAssignmentService {
	return &TicketAssignmentService{s, ids}
}
func (s *TicketAssignmentService) ReadTicketAssignment(ctx context.Context, p authz.Principal, id string) (TicketAssignment, error) {
	if p.ValidateUser() != nil || entityref.M0Registry().Validate(entityref.Ref{Type: "ticket", ID: id}) != nil {
		return TicketAssignment{}, authz.ErrInvalid
	}
	return s.store.ReadTicketAssignment(ctx, p, id)
}
func (s *TicketAssignmentService) ListAssigneeCandidates(ctx context.Context, p authz.Principal, id string, q DiscoveryPageInput) (AssigneeCandidates, error) {
	if p.ValidateUser() != nil || entityref.M0Registry().Validate(entityref.Ref{Type: "ticket", ID: id}) != nil || q.Limit < 1 || q.Limit > MaxDiscoveryPageSize || q.AfterID != "" && !ValidConfigurationID(q.AfterID, "usr_") {
		return AssigneeCandidates{}, authz.ErrInvalid
	}
	return s.store.ListAssigneeCandidates(ctx, p, id, q)
}
func (s *TicketAssignmentService) SetTicketAssignee(ctx context.Context, inv Invocation, in TicketAssignmentInput) (TicketAssignmentResult, error) {
	if err := validateInvocation(inv); err != nil {
		return TicketAssignmentResult{}, err
	}
	if inv.SourceKind != "web" {
		return TicketAssignmentResult{}, authz.ErrInvalid
	}
	if err := ValidateTicketAssignmentInput(in); err != nil {
		return TicketAssignmentResult{}, err
	}
	event, err := s.ids.NewID("evt_")
	if err != nil {
		return TicketAssignmentResult{}, err
	}
	payload := in
	payload.ClientOperationID = ""
	return s.store.SetTicketAssignee(ctx, TicketAssignmentCommand{Invocation: inv, TicketAssignmentInput: in, EventID: event, PayloadSHA256: collaborationPayloadDigest(payload)})
}

func ValidateTicketAssignmentInput(in TicketAssignmentInput) error {
	lifecycle, e1 := strconv.ParseInt(in.TargetLifecycleRevision, 10, 64)
	project, e2 := strconv.ParseInt(in.ProjectManagementRevision, 10, 64)
	if entityref.M0Registry().Validate(entityref.Ref{Type: "ticket", ID: in.TicketID}) != nil || !validClientOperationID(in.ClientOperationID) || !in.Confirmed || !ValidTicketStatusRevision(in.BaseAssignmentRevision) || !ValidTicketStatusRevision(in.BaseStatusRevision) || e1 != nil || e2 != nil || project < 1 || strconv.FormatInt(project, 10) != in.ProjectManagementRevision || strconv.FormatInt(lifecycle, 10) != in.TargetLifecycleRevision || (in.AssigneeUserID == nil && lifecycle != 0) || (in.AssigneeUserID != nil && (!ValidConfigurationID(*in.AssigneeUserID, "usr_") || lifecycle < 1)) {
		return authz.ErrInvalid
	}
	return nil
}
