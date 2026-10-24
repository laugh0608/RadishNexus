package goldenpath

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

var ErrManagementUnconfirmed = errors.New("membership management result unconfirmed")

// ManagementProposal is a closed command, not a policy expression. Scope and
// action are supplied by dedicated transport routes, not by JSON authority.
type ManagementProposal struct {
	ScopeKind      string `json:"scope_kind"`
	ScopeID        string `json:"scope_id"`
	Action         string `json:"action"`
	TargetUserID   string `json:"target_user_id"`
	Role           string `json:"role"`
	ActorAfterRole string `json:"actor_after_role"`
}

func (p ManagementProposal) Kind() string { return "management." + p.ScopeKind + "." + p.Action }

type ManagementInput struct {
	ManagementProposal
	ClientOperationID string `json:"client_operation_id"`
	PlanDigest        string `json:"plan_digest"`
	Confirmed         bool   `json:"confirmed"`
}
type ManagementPlan struct {
	ManagementProposal
	ActorID                 string `json:"actor_id"`
	TargetName              string `json:"target_name"`
	Before                  string `json:"before"`
	After                   string `json:"after"`
	ActorRole               string `json:"actor_role"`
	TargetWorkspaceRole     string `json:"target_workspace_role"`
	TargetStatus            string `json:"target_status"`
	ScopeRevision           string `json:"scope_revision"`
	ActorLifecycleRevision  string `json:"actor_lifecycle_revision"`
	TargetLifecycleRevision string `json:"target_lifecycle_revision"`
	ActorRoleRevision       string `json:"actor_role_revision"`
	TargetRoleRevision      string `json:"target_role_revision"`
	ParentProjectRevision   string `json:"parent_project_revision"`
	ParentChannelRevision   string `json:"parent_channel_revision"`
	Policy                  string `json:"policy"`
	Digest                  string `json:"digest"`
}

// The plan contains only readable scope facts and fixed policy. It must never
// incorporate hidden descendant IDs, counts or an aggregate topology epoch.
func (p ManagementPlan) SHA256(workspace string) (string, error) {
	p.Digest = ""
	body, err := json.Marshal(struct {
		Version   int
		Workspace string
		Plan      ManagementPlan
	}{1, workspace, p})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

type ManagementCommand struct {
	Access                            OwnerAccess
	Input                             ManagementInput
	AuditID, RequestID, PayloadSHA256 string
	InvitationID, InvitationToken     string
	InvitationDigest                  []byte
}
type ManagementInvitation struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}
type ManagementResult struct {
	Invitation        *ManagementInvitation `json:"invitation,omitempty"`
	Result            string                `json:"result"`
	WorkspaceID       string                `json:"workspace_id"`
	ClientOperationID string                `json:"client_operation_id"`
	Action            string                `json:"action,omitempty"`
	ScopeID           string                `json:"scope_id,omitempty"`
	TargetUserID      string                `json:"target_user_id,omitempty"`
	OccurredAt        *time.Time            `json:"occurred_at,omitempty"`
}
type ManagementMember struct {
	ID                string `json:"id"`
	DisplayName       string `json:"display_name"`
	Status            string `json:"status"`
	Role              string `json:"role"`
	LifecycleRevision string `json:"lifecycle_revision"`
	Eligible          bool   `json:"eligible"`
}
type ManagementPage struct {
	Items         []ManagementMember `json:"items"`
	NextID        string             `json:"-"`
	CanManage     bool               `json:"can_manage"`
	SelfRole      string             `json:"self_role"`
	ScopeRevision string             `json:"scope_revision"`
}
type ManagementQuery struct {
	ScopeKind, ScopeID, AfterID string
	Candidates                  bool
	Limit                       int
}
type ManagementAudit struct {
	ID           string    `json:"id"`
	ActorID      string    `json:"actor_id"`
	TargetUserID string    `json:"target_user_id"`
	Action       string    `json:"action"`
	Before       string    `json:"before"`
	After        string    `json:"after"`
	OccurredAt   time.Time `json:"occurred_at"`
}
type ManagementAuditPage struct {
	Items  []ManagementAudit
	NextID string
}
type ManagementResponsibility struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	Role string `json:"role"`
}
type ManagementResponsibilityPage struct {
	Items  []ManagementResponsibility
	NextID string
}
type ManagementStore interface {
	ListManagementResponsibilities(context.Context, OwnerAccess, string, int) (ManagementResponsibilityPage, error)
	PreviewManagement(context.Context, OwnerAccess, ManagementProposal) (ManagementPlan, error)
	ApplyManagement(context.Context, ManagementCommand) (ManagementResult, error)
	ReadManagementResult(context.Context, OwnerAccess, string) (ManagementResult, error)
	ListManagementMembers(context.Context, OwnerAccess, ManagementQuery) (ManagementPage, error)
	ListManagementAudit(context.Context, OwnerAccess, ManagementQuery) (ManagementAuditPage, error)
}

func (s *ManagementService) Responsibilities(ctx context.Context, p authz.Principal, token, after string, limit int) (ManagementResponsibilityPage, error) {
	a, err := ownerAccess(p, token)
	if err != nil {
		return ManagementResponsibilityPage{}, err
	}
	if limit < 1 || limit > 50 || (after != "" && !ValidManagementScope("project", after, p.WorkspaceID) && !ValidManagementScope("channel", after, p.WorkspaceID) && !ValidManagementScope("thread", after, p.WorkspaceID)) {
		return ManagementResponsibilityPage{}, authz.ErrInvalid
	}
	return s.store.ListManagementResponsibilities(ctx, a, after, limit)
}

type ManagementService struct {
	store ManagementStore
	ids   IDGenerator
}

func NewManagementService(store ManagementStore, ids IDGenerator) *ManagementService {
	return &ManagementService{store, ids}
}

func ValidManagementScope(kind, id, workspace string) bool {
	switch kind {
	case "workspace":
		return id == workspace && ValidConfigurationID(id, "wrk_")
	case "project":
		return ValidConfigurationID(id, "prj_")
	case "channel":
		return ValidConfigurationID(id, "chn_")
	case "thread":
		return len(id) <= 128 && strings.HasPrefix(id, "thr_") && managementThreadID(id)
	}
	return false
}
func managementThreadID(id string) bool {
	for _, c := range strings.TrimPrefix(id, "thr_") {
		if c != '_' && c != '-' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return len(id) > 4
}
func ValidateManagementProposal(p authz.Principal, in ManagementProposal) error {
	if !ValidManagementScope(in.ScopeKind, in.ScopeID, p.WorkspaceID) || !ValidConfigurationID(in.TargetUserID, "usr_") {
		return authz.ErrInvalid
	}
	if in.Action == "leave" && in.TargetUserID != p.ID {
		return authz.ErrInvalid
	}
	if in.Action != "handoff" && in.ActorAfterRole != "" {
		return authz.ErrInvalid
	}
	if in.Action != "role" && !(in.ScopeKind == "project" && in.Action == "add") && in.Role != "" {
		return authz.ErrInvalid
	}
	switch in.ScopeKind {
	case "workspace":
		if in.Action != "suspend" && in.Action != "resume" && in.Action != "remove" && in.Action != "leave" && in.Action != "reinvite" {
			return authz.ErrInvalid
		}
	case "project":
		if in.Action != "add" && in.Action != "role" && in.Action != "remove" && in.Action != "handoff" && in.Action != "leave" {
			return authz.ErrInvalid
		}
		if (in.Action == "role" || in.Action == "add") && in.Role != "viewer" && in.Role != "contributor" && in.Role != "decider" && in.Role != "admin" {
			return authz.ErrInvalid
		}
		if in.Action == "add" && in.Role == "admin" {
			return authz.ErrInvalid
		}
		if in.Action == "handoff" && in.ActorAfterRole != "" && in.ActorAfterRole != "viewer" && in.ActorAfterRole != "contributor" && in.ActorAfterRole != "decider" {
			return authz.ErrInvalid
		}
	case "channel", "thread":
		if in.Action != "add" && in.Action != "role" && in.Action != "remove" && in.Action != "handoff" && in.Action != "leave" {
			return authz.ErrInvalid
		}
		if in.Action == "role" && in.Role != "member" && in.Role != "manager" {
			return authz.ErrInvalid
		}
		if in.Action == "handoff" && in.ActorAfterRole != "" && in.ActorAfterRole != "member" {
			return authz.ErrInvalid
		}
	}
	if (in.Action == "handoff" || in.Action == "suspend" || in.Action == "remove" || in.Action == "role") && in.TargetUserID == p.ID {
		return authz.ErrInvalid
	}
	return nil
}
func (s *ManagementService) Preview(ctx context.Context, p authz.Principal, token string, in ManagementProposal) (ManagementPlan, error) {
	a, err := ownerAccess(p, token)
	if err != nil {
		return ManagementPlan{}, err
	}
	if err = ValidateManagementProposal(p, in); err != nil {
		return ManagementPlan{}, err
	}
	return s.store.PreviewManagement(ctx, a, in)
}
func (s *ManagementService) Apply(ctx context.Context, v Invocation, token string, in ManagementInput) (ManagementResult, error) {
	a, err := ownerAccess(v.Principal, token)
	if err != nil {
		return ManagementResult{}, err
	}
	if err = ValidateManagementProposal(v.Principal, in.ManagementProposal); err != nil {
		return ManagementResult{}, err
	}
	if err = ValidateManagementInput(v.Principal, in); err != nil {
		return ManagementResult{}, err
	}
	body, err := json.Marshal(struct {
		Workspace, Actor string
		Input            ManagementInput
	}{a.Principal.WorkspaceID, a.Principal.ID, in})
	if err != nil {
		return ManagementResult{}, err
	}
	sum := sha256.Sum256(body)
	id, err := s.ids.NewID("cfa_")
	if err != nil {
		return ManagementResult{}, err
	}
	command := ManagementCommand{Access: a, Input: in, AuditID: id, RequestID: v.CorrelationID, PayloadSHA256: hex.EncodeToString(sum[:])}
	if in.Action == "reinvite" {
		secret := make([]byte, 32)
		if _, err = rand.Read(secret); err != nil {
			return ManagementResult{}, err
		}
		command.InvitationToken = base64.RawURLEncoding.EncodeToString(secret)
		d := sha256.Sum256([]byte(command.InvitationToken))
		command.InvitationDigest = d[:]
		command.InvitationID, err = s.ids.NewID("inv_")
		if err != nil {
			return ManagementResult{}, err
		}
	}
	return s.store.ApplyManagement(ctx, command)
}
func (s *ManagementService) Result(ctx context.Context, p authz.Principal, token, operation string) (ManagementResult, error) {
	a, err := ownerAccess(p, token)
	if err != nil {
		return ManagementResult{}, err
	}
	if !ValidOwnerOperationID(operation) {
		return ManagementResult{}, authz.ErrInvalid
	}
	return s.store.ReadManagementResult(ctx, a, operation)
}
func validateManagementQuery(p authz.Principal, q ManagementQuery, audit bool) error {
	if !ValidManagementScope(q.ScopeKind, q.ScopeID, p.WorkspaceID) || q.Limit < 1 || q.Limit > 50 {
		return authz.ErrInvalid
	}
	if q.AfterID != "" {
		if audit {
			if len(q.AfterID) > 128 || !strings.HasPrefix(q.AfterID, "cfa_") {
				return authz.ErrInvalid
			}
		} else if !ValidConfigurationID(q.AfterID, "usr_") {
			return authz.ErrInvalid
		}
	}
	return nil
}
func (s *ManagementService) Members(ctx context.Context, p authz.Principal, token string, q ManagementQuery) (ManagementPage, error) {
	a, err := ownerAccess(p, token)
	if err != nil {
		return ManagementPage{}, err
	}
	if err = validateManagementQuery(p, q, false); err != nil {
		return ManagementPage{}, err
	}
	return s.store.ListManagementMembers(ctx, a, q)
}
func (s *ManagementService) Audit(ctx context.Context, p authz.Principal, token string, q ManagementQuery) (ManagementAuditPage, error) {
	a, err := ownerAccess(p, token)
	if err != nil {
		return ManagementAuditPage{}, err
	}
	if err = validateManagementQuery(p, q, true); err != nil {
		return ManagementAuditPage{}, err
	}
	return s.store.ListManagementAudit(ctx, a, q)
}

func ValidateManagementInput(p authz.Principal, in ManagementInput) error {
	if err := ValidateManagementProposal(p, in.ManagementProposal); err != nil {
		return err
	}
	digest, e := hex.DecodeString(in.PlanDigest)
	if !in.Confirmed || !ValidOwnerOperationID(in.ClientOperationID) || e != nil || len(digest) != 32 || hex.EncodeToString(digest) != in.PlanDigest {
		return authz.ErrInvalid
	}
	return nil
}
