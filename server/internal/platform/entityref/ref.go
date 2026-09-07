// Package entityref implements the canonical EntityRef representation.
package entityref

import (
	"errors"
	"fmt"
	"strings"
)

const scheme = "entity://"

var (
	ErrInvalidReference = errors.New("invalid entity reference")
	ErrUnknownType      = errors.New("unknown entity type")
)

// Ref identifies an entity inside a separately supplied Workspace context.
type Ref struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Registry contains accepted entity type and ID-prefix pairs.
type Registry map[string]string

// M0Registry returns a new registry containing the frozen M0 entity types.
func M0Registry() Registry {
	return Registry{
		"project":     "prj_",
		"initiative":  "ini_",
		"component":   "cmp_",
		"channel":     "chn_",
		"decision":    "dec_",
		"environment": "env_",
		"entity-link": "lnk_",
		"message":     "msg_",
		"thread":      "thr_",
		"ticket":      "tkt_",
		"ci-run":      "cir_",
		"deployment":  "dpl_",
	}
}

// Parse accepts only the canonical entity://<type>/<id> form.
func Parse(raw string, registry Registry) (Ref, error) {
	if !strings.HasPrefix(raw, scheme) {
		return Ref{}, fmt.Errorf("%w: missing %s scheme", ErrInvalidReference, scheme)
	}

	remainder := strings.TrimPrefix(raw, scheme)
	if strings.ContainsAny(remainder, "?#") {
		return Ref{}, fmt.Errorf("%w: query parameters and fragments are forbidden", ErrInvalidReference)
	}

	parts := strings.Split(remainder, "/")
	if len(parts) != 2 {
		return Ref{}, fmt.Errorf("%w: expected exactly one type and ID segment", ErrInvalidReference)
	}

	ref := Ref{Type: parts[0], ID: parts[1]}
	if err := registry.Validate(ref); err != nil {
		return Ref{}, err
	}
	return ref, nil
}

// Validate checks the structured EntityRef form without normalizing it.
func (registry Registry) Validate(ref Ref) error {
	prefix, ok := registry[ref.Type]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownType, ref.Type)
	}
	if !isCanonicalToken(ref.Type) || !isCanonicalToken(ref.ID) || strings.ContainsAny(ref.ID, "?#") {
		return fmt.Errorf("%w: type and ID must be non-empty ASCII tokens", ErrInvalidReference)
	}
	if !strings.HasPrefix(ref.ID, prefix) {
		return fmt.Errorf("%w: ID %q does not use %q prefix", ErrInvalidReference, ref.ID, prefix)
	}
	return nil
}

// URI returns the canonical textual representation.
func (ref Ref) URI() string {
	return scheme + ref.Type + "/" + ref.ID
}

func isCanonicalToken(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char > 0x7f || char <= 0x20 || char == '/' {
			return false
		}
	}
	return true
}
