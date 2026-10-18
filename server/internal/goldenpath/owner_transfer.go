package goldenpath

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

var ErrOwnerTransferUnconfirmed = errors.New("owner transfer result unconfirmed")

var ownerOperationID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func ValidOwnerOperationID(value string) bool { return ownerOperationID.MatchString(value) }

func ParseRoleRevision(value string) (int64, error) {
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision <= 0 || strconv.FormatInt(revision, 10) != value {
		return 0, authz.ErrInvalid
	}
	return revision, nil
}

// OwnerAccess is constructed from the verified browser session, never JSON.
// The Store independently rechecks the session and current membership.
type OwnerAccess struct {
	Principal     authz.Principal
	SessionDigest [32]byte `json:"-"`
}

type Ownership struct {
	UserID, Role          string
	RoleRevision          int64
	RecentlyAuthenticated bool
}

type OwnerCandidate struct {
	ID, DisplayName string
	RoleRevision    int64
}

type OwnerCandidatePage struct {
	Items  []OwnerCandidate
	NextID string
}

type OwnerTransferInput struct {
	ClientOperationID          string `json:"client_operation_id"`
	TargetUserID               string `json:"target_user_id"`
	ExpectedActorRoleRevision  string `json:"expected_actor_role_revision"`
	ExpectedTargetRoleRevision string `json:"expected_target_role_revision"`
	Confirmed                  bool   `json:"confirmed"`
}

type OwnerTransferCommand struct {
	Access                            OwnerAccess
	Input                             OwnerTransferInput
	AuditID, RequestID, PayloadSHA256 string
	ActorRevision, TargetRevision     int64
}

type OwnerTransferResult struct {
	Result, WorkspaceID, ClientOperationID string
	ActorUserID, TargetUserID, AuditID     string
	OccurredAt                             time.Time
	ActorRoleRevision, TargetRoleRevision  int64
}

type OwnerTransferStore interface {
	ReadOwnership(context.Context, OwnerAccess) (Ownership, error)
	ListOwnerCandidates(context.Context, OwnerAccess, string, int) (OwnerCandidatePage, error)
	TransferOwner(context.Context, OwnerTransferCommand) (OwnerTransferResult, error)
	ReadOwnerTransfer(context.Context, OwnerAccess, string) (OwnerTransferResult, error)
}

type OwnerTransferService struct {
	store OwnerTransferStore
	ids   IDGenerator
}

func NewOwnerTransferService(store OwnerTransferStore, ids IDGenerator) *OwnerTransferService {
	return &OwnerTransferService{store: store, ids: ids}
}

func ownerAccess(p authz.Principal, token string) (OwnerAccess, error) {
	if err := p.ValidateUser(); err != nil {
		return OwnerAccess{}, err
	}
	if !ValidConfigurationID(p.WorkspaceID, "wrk_") || !ValidConfigurationID(p.ID, "usr_") || token == "" {
		return OwnerAccess{}, authz.ErrInvalid
	}
	return OwnerAccess{Principal: p, SessionDigest: sha256.Sum256([]byte(token))}, nil
}

func (s *OwnerTransferService) Ownership(ctx context.Context, p authz.Principal, token string) (Ownership, error) {
	a, err := ownerAccess(p, token)
	if err != nil {
		return Ownership{}, err
	}
	return s.store.ReadOwnership(ctx, a)
}

func (s *OwnerTransferService) Candidates(ctx context.Context, p authz.Principal, token, after string, limit int) (OwnerCandidatePage, error) {
	a, err := ownerAccess(p, token)
	if err != nil {
		return OwnerCandidatePage{}, err
	}
	if limit < 1 || limit > 50 || (after != "" && !ValidConfigurationID(after, "usr_")) {
		return OwnerCandidatePage{}, authz.ErrInvalid
	}
	return s.store.ListOwnerCandidates(ctx, a, after, limit)
}

func (s *OwnerTransferService) Status(ctx context.Context, p authz.Principal, token, operation string) (OwnerTransferResult, error) {
	a, err := ownerAccess(p, token)
	if err != nil {
		return OwnerTransferResult{}, err
	}
	if !ValidOwnerOperationID(operation) {
		return OwnerTransferResult{}, authz.ErrInvalid
	}
	return s.store.ReadOwnerTransfer(ctx, a, operation)
}

func (s *OwnerTransferService) Transfer(ctx context.Context, invocation Invocation, token string, input OwnerTransferInput) (OwnerTransferResult, error) {
	a, err := ownerAccess(invocation.Principal, token)
	if err != nil {
		return OwnerTransferResult{}, err
	}
	actorRevision, actorErr := ParseRoleRevision(input.ExpectedActorRoleRevision)
	targetRevision, targetErr := ParseRoleRevision(input.ExpectedTargetRoleRevision)
	if !input.Confirmed || !ValidOwnerOperationID(input.ClientOperationID) || !ValidConfigurationID(input.TargetUserID, "usr_") || input.TargetUserID == a.Principal.ID || actorErr != nil || targetErr != nil {
		return OwnerTransferResult{}, authz.ErrInvalid
	}
	// A closed, versioned shape, independent of transport and session material.
	body, err := json.Marshal(struct {
		Version                    int
		Kind, WorkspaceID, ActorID string
		Input                      OwnerTransferInput
	}{1, "workspace.owner.transfer", a.Principal.WorkspaceID, a.Principal.ID, input})
	if err != nil {
		return OwnerTransferResult{}, err
	}
	digest := sha256.Sum256(body)
	id, err := s.ids.NewID("cfa_")
	if err != nil {
		return OwnerTransferResult{}, err
	}
	return s.store.TransferOwner(ctx, OwnerTransferCommand{Access: a, Input: input, AuditID: id, RequestID: invocation.CorrelationID, PayloadSHA256: hex.EncodeToString(digest[:]), ActorRevision: actorRevision, TargetRevision: targetRevision})
}
