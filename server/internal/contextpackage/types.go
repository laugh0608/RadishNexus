// Package contextpackage defines the portable, credential-free collaboration
// profile. It contains no SQL, authentication adapter or database table format.
package contextpackage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

const (
	Format           = "radishnexus.context"
	Profile          = "project-collaboration-v1"
	Version          = 1
	MaxBytes         = 64 << 20
	MaxObjects       = 10000
	MaxHistory       = 50000
	MaxLineBytes     = 1 << 20
	MaxLineage       = 8
	PermissionPolicy = "restricted-project-importer-only-v1"
)

var (
	ErrInvalid     = errors.New("invalid context package")
	ErrIncomplete  = errors.New("context cannot be exported within the authorized profile")
	ErrUnconfirmed = errors.New("context import outcome unconfirmed")
)

type Ref struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
type Origin struct {
	PackageID string `json:"package_id"`
	Digest    string `json:"package_digest"`
	SourceID  string `json:"source_id"`
}
type Actor struct {
	ID      string   `json:"id"`
	Lineage []Origin `json:"lineage"`
}
type Object struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Actor     string          `json:"actor"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Data      json.RawMessage `json:"data"`
	Lineage   []Origin        `json:"lineage"`
}
type Team struct {
	Name string `json:"name"`
}
type Project struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	Summary    string `json:"summary"`
	TeamID     string `json:"team_id"`
	Visibility string `json:"visibility"`
	Status     string `json:"status"`
}
type Channel struct {
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
	Status     string `json:"status"`
}
type Message struct {
	ChannelID string  `json:"channel_id"`
	ThreadID  *string `json:"thread_id"`
	Body      string  `json:"body"`
}
type Thread struct {
	Title      string  `json:"title"`
	Visibility string  `json:"visibility"`
	ChannelID  *string `json:"channel_id"`
}
type Decision struct {
	Question     string          `json:"question"`
	Outcome      *string         `json:"outcome"`
	Rationale    *string         `json:"rationale"`
	Status       string          `json:"status"`
	Proposer     string          `json:"proposer"`
	Deciders     []string        `json:"deciders"`
	DecidedAt    *time.Time      `json:"decided_at"`
	Alternatives json.RawMessage `json:"alternatives"`
	Consequences *string         `json:"consequences"`
	ReviewAt     *time.Time      `json:"review_at"`
}
type Ticket struct {
	Title          string `json:"title"`
	Status         string `json:"status"`
	StatusRevision int    `json:"status_revision"`
}
type Document struct {
	Revision   int    `json:"revision"`
	Visibility string `json:"visibility"`
}
type Revision struct {
	DocumentID   string    `json:"document_id"`
	Revision     int       `json:"revision"`
	Title        string    `json:"title"`
	Format       string    `json:"format"`
	Actor        string    `json:"actor"`
	CreatedAt    time.Time `json:"created_at"`
	EventID      string    `json:"event_id"`
	RestoredFrom *int      `json:"restored_from_revision"`
	BodyFile     string    `json:"body_file"`
}
type Link struct {
	ID            string     `json:"id"`
	From          Ref        `json:"from"`
	To            Ref        `json:"to"`
	Relation      string     `json:"relation"`
	Assertion     string     `json:"assertion"`
	Origin        string     `json:"origin"`
	Actor         string     `json:"actor"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	EventID       string     `json:"event_id"`
	State         string     `json:"state"`
	RemovedActor  *string    `json:"removed_actor"`
	RemovedAt     *time.Time `json:"removed_at"`
	RemovalReason *string    `json:"removal_reason"`
	Lineage       []Origin   `json:"lineage"`
}
type History struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	Version       int             `json:"version"`
	Entity        Ref             `json:"entity"`
	Actor         string          `json:"actor"`
	Source        string          `json:"source"`
	OccurredAt    time.Time       `json:"occurred_at"`
	CorrelationID string          `json:"correlation_id"`
	CausationID   *string         `json:"causation_id"`
	Facts         json.RawMessage `json:"facts"`
	Lineage       []Origin        `json:"lineage"`
}

// Facts is a whitelist. Validate rejects fields that do not belong to the
// particular event, even when their values happen to be zero.
type Facts struct {
	Status       string `json:"status,omitempty"`
	Evidence     *Ref   `json:"evidence,omitempty"`
	Decision     *Ref   `json:"decision,omitempty"`
	Ticket       *Ref   `json:"ticket,omitempty"`
	Channel      *Ref   `json:"channel,omitempty"`
	Thread       *Ref   `json:"thread,omitempty"`
	Message      *Ref   `json:"message,omitempty"`
	Revision     int    `json:"revision,omitempty"`
	RestoredFrom *int   `json:"restored_from_revision,omitempty"`
	FromStatus   string `json:"from_status,omitempty"`
	ToStatus     string `json:"to_status,omitempty"`
	FromRevision int    `json:"from_status_revision,omitempty"`
	ToRevision   int    `json:"to_status_revision,omitempty"`
}
type Descriptor struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	Lines  int    `json:"lines"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	Format        string       `json:"format"`
	Version       int          `json:"format_version"`
	Profile       string       `json:"profile"`
	PackageID     string       `json:"package_id"`
	CreatedAt     time.Time    `json:"created_at"`
	SourceVersion string       `json:"source_version"`
	WorkspaceID   string       `json:"source_workspace_id"`
	ProjectID     string       `json:"source_project_id"`
	Files         []Descriptor `json:"files"`
}
type File struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
}

// Bundle is only the HTTP transfer envelope. On disk every File is stored at
// its manifest path; the package contains readable JSONL and Markdown.
type Bundle struct {
	Manifest Manifest `json:"manifest"`
	Files    []File   `json:"files"`
}
type Data struct {
	Objects   []Object
	Actors    []Actor
	Links     []Link
	History   []History
	Revisions []Revision
	Bodies    map[string]string
}
type Mapping struct {
	Kind     string `json:"kind"`
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id"`
}
type Plan struct {
	OperationID      string    `json:"operation_id"`
	PackageID        string    `json:"package_id"`
	PackageDigest    string    `json:"package_digest"`
	PlanDigest       string    `json:"plan_digest"`
	WorkspaceID      string    `json:"workspace_id"`
	TargetIdentity   string    `json:"target_identity"`
	ActorID          string    `json:"actor_id"`
	Policy           string    `json:"permission_policy"`
	Objects          int       `json:"objects"`
	History          int       `json:"history"`
	Revisions        int       `json:"document_revisions"`
	HistoricalActors int       `json:"historical_actors"`
	Mappings         []Mapping `json:"mappings"`
}
type Result struct {
	Status        string     `json:"status"`
	OperationID   string     `json:"operation_id"`
	PackageDigest string     `json:"package_digest,omitempty"`
	PlanDigest    string     `json:"plan_digest,omitempty"`
	ProjectID     string     `json:"project_id,omitempty"`
	AppliedAt     *time.Time `json:"applied_at,omitempty"`
}

func Hash(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func NewID(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b[:]), nil
}
func Encode(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic("context package contains non-JSON data")
	}
	return b
}
func Digest(m Manifest) string { return Hash(Encode(m)) }
func Prefix(kind string) string {
	return map[string]string{"team": "tem_", "project": "prj_", "channel": "chn_", "message": "msg_", "thread": "thr_", "decision": "dec_", "ticket": "tkt_", "document": "doc_", "entity-link": "lnk_", "event": "evt_", "actor": "usr_"}[kind]
}
