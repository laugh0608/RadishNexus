package goldenpath

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

// ConfigurationInput is the closed set of foundation configuration commands.
// Transport maps dedicated routes to Kind; callers cannot supply authority.
type ConfigurationInput struct {
	Kind               string
	ScopeID            string
	UserID             string
	ClientOperationID  string
	Name               string
	Key                string
	OwnerTeamID        string
	Visibility         string
	InitialAdminUserID string
	MemberUserIDs      []string
	Role               string
	ExpectedRole       *string
	ExpectedMember     bool
}

type ConfigurationCommand struct {
	Invocation
	ConfigurationInput
	ID, AuditID, EventID, PayloadSHA256 string
	OccurredAt                          time.Time
}

type ConfigurationObject struct {
	ID, Kind, Name, Key, ProjectID, Visibility, Status string
	CanManage                                          bool
}
type ConfigurationResult struct {
	Object  ConfigurationObject
	UserID  string
	Created bool
}
type ConfigurationMember struct {
	ID, Name, Role string
	Eligible       bool
}
type ConfigurationPage struct {
	Members []ConfigurationMember
	Teams   []ConfigurationObject
	NextID  string
}
type ConfigurationQuery struct {
	Kind, ScopeID, AfterID string
	Limit                  int
}
type ConfigurationStore interface {
	Configure(context.Context, ConfigurationCommand) (ConfigurationResult, error)
	ReadConfiguration(context.Context, authz.Principal, string, string) (ConfigurationObject, error)
	ListConfiguration(context.Context, authz.Principal, ConfigurationQuery) (ConfigurationPage, error)
}
type ConfigurationService struct {
	store ConfigurationStore
	ids   IDGenerator
	clock Clock
}

func NewConfigurationService(store ConfigurationStore, ids IDGenerator, clock Clock) *ConfigurationService {
	return &ConfigurationService{store: store, ids: ids, clock: clock}
}

var configurationID = regexp.MustCompile(`^(wrk|usr|tem|prj|chn)_[A-Za-z0-9_-]+$`)
var projectKey = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func ValidConfigurationID(id, prefix string) bool {
	return len(id) <= 128 && strings.HasPrefix(id, prefix) && configurationID.MatchString(id)
}
func (s *ConfigurationService) Configure(ctx context.Context, invocation Invocation, input ConfigurationInput) (ConfigurationResult, error) {
	if err := invocation.Principal.ValidateUser(); err != nil {
		return ConfigurationResult{}, err
	}
	invalid := func() (ConfigurationResult, error) {
		return ConfigurationResult{}, fmt.Errorf("%w: invalid configuration command", authz.ErrInvalid)
	}
	if !ValidConfigurationID(invocation.Principal.WorkspaceID, "wrk_") || !ValidConfigurationID(invocation.Principal.ID, "usr_") || !validClientOperationID(input.ClientOperationID) {
		return invalid()
	}
	prefix := ""
	switch input.Kind {
	case "team.create", "project.create":
		if input.ScopeID != invocation.Principal.WorkspaceID {
			return invalid()
		}
		prefix = "tem_"
		if input.Kind == "project.create" {
			prefix = "prj_"
			if !projectKey.MatchString(input.Key) || !ValidConfigurationID(input.OwnerTeamID, "tem_") || input.InitialAdminUserID != invocation.Principal.ID || (input.Visibility != "workspace" && input.Visibility != "restricted") {
				return invalid()
			}
		}
	case "channel.create":
		prefix = "chn_"
		if !ValidConfigurationID(input.ScopeID, "prj_") || (input.Visibility != "project" && input.Visibility != "restricted") || input.MemberUserIDs == nil {
			return invalid()
		}
		input.MemberUserIDs = slices.Clone(input.MemberUserIDs)
		slices.Sort(input.MemberUserIDs)
		if input.Visibility == "project" && len(input.MemberUserIDs) != 0 {
			return invalid()
		}
		if input.Visibility == "restricted" && (len(input.MemberUserIDs) == 0 || len(input.MemberUserIDs) > 50 || !slices.Contains(input.MemberUserIDs, invocation.Principal.ID)) {
			return invalid()
		}
		for i, id := range input.MemberUserIDs {
			if !ValidConfigurationID(id, "usr_") || (i > 0 && id == input.MemberUserIDs[i-1]) {
				return invalid()
			}
		}
	case "project.member.set", "project.member.remove":
		if !ValidConfigurationID(input.ScopeID, "prj_") || !ValidConfigurationID(input.UserID, "usr_") {
			return invalid()
		}
		if input.Kind == "project.member.set" && input.Role != "viewer" && input.Role != "contributor" && input.Role != "decider" {
			return invalid()
		}
		if input.ExpectedRole != nil && *input.ExpectedRole != "viewer" && *input.ExpectedRole != "contributor" && *input.ExpectedRole != "decider" && *input.ExpectedRole != "admin" {
			return invalid()
		}
	case "channel.member.add", "channel.member.remove":
		if !ValidConfigurationID(input.ScopeID, "chn_") || !ValidConfigurationID(input.UserID, "usr_") {
			return invalid()
		}
	default:
		return invalid()
	}
	if prefix != "" {
		input.Name = strings.TrimSpace(input.Name)
		if input.Name == "" || len(input.Name) > 480 || !utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name) > 120 || strings.ContainsFunc(input.Name, unicode.IsControl) {
			return invalid()
		}
	}
	body, err := json.Marshal(input)
	if err != nil {
		return ConfigurationResult{}, err
	}
	digest := sha256.Sum256(body)
	command := ConfigurationCommand{Invocation: invocation, ConfigurationInput: input, PayloadSHA256: hex.EncodeToString(digest[:]), OccurredAt: s.clock.Now().UTC()}
	command.AuditID, err = s.ids.NewID("cfa_")
	if err != nil {
		return ConfigurationResult{}, err
	}
	if prefix != "" {
		command.ID, err = s.ids.NewID(prefix)
		if err != nil {
			return ConfigurationResult{}, err
		}
	}
	if prefix == "prj_" || prefix == "chn_" {
		command.EventID, err = s.ids.NewID("evt_")
		if err != nil {
			return ConfigurationResult{}, err
		}
	}
	return s.store.Configure(ctx, command)
}
func (s *ConfigurationService) ReadConfiguration(ctx context.Context, p authz.Principal, kind, id string) (ConfigurationObject, error) {
	if err := p.ValidateUser(); err != nil {
		return ConfigurationObject{}, err
	}
	prefix := "prj_"
	if kind == "channel" {
		prefix = "chn_"
	} else if kind != "project" {
		return ConfigurationObject{}, authz.ErrInvalid
	}
	if !ValidConfigurationID(id, prefix) {
		return ConfigurationObject{}, authz.ErrInvalid
	}
	return s.store.ReadConfiguration(ctx, p, kind, id)
}
func (s *ConfigurationService) ListConfiguration(ctx context.Context, p authz.Principal, q ConfigurationQuery) (ConfigurationPage, error) {
	if err := p.ValidateUser(); err != nil {
		return ConfigurationPage{}, err
	}
	prefix := "usr_"
	switch q.Kind {
	case "teams":
		prefix = "tem_"
		if q.ScopeID != p.WorkspaceID {
			return ConfigurationPage{}, authz.ErrInvalid
		}
	case "members":
		if q.ScopeID != p.WorkspaceID {
			return ConfigurationPage{}, authz.ErrInvalid
		}
	case "project-members":
		if !ValidConfigurationID(q.ScopeID, "prj_") {
			return ConfigurationPage{}, authz.ErrInvalid
		}
	case "channel-members":
		if !ValidConfigurationID(q.ScopeID, "chn_") {
			return ConfigurationPage{}, authz.ErrInvalid
		}
	default:
		return ConfigurationPage{}, authz.ErrInvalid
	}
	if q.Limit < 1 || q.Limit > 50 || (q.AfterID != "" && !ValidConfigurationID(q.AfterID, prefix)) {
		return ConfigurationPage{}, authz.ErrInvalid
	}
	return s.store.ListConfiguration(ctx, p, q)
}
