package jenkins

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const MaxConfig = 64 * 1024

var scopedID = regexp.MustCompile(`^(wrk|cmp)_[A-Za-z0-9_-]+$`)
var secretPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var ErrConfiguration = errors.New("invalid Jenkins configuration or secret file")

type Source struct {
	ID          string
	WorkspaceID string
	ComponentID string
	Job         string
	keys        map[string][]byte
}

// String deliberately omits bindings and key material in accidental formatting.
func (s Source) String() string   { return "Jenkins source (redacted)" }
func (s Source) GoString() string { return s.String() }

// ReadFile bounds allocation even for oversized files. Errors omit paths.
func ReadFile(path string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrConfiguration
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrConfiguration
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, ErrConfiguration
	}
	return body, nil
}

type FileReader func(string, int64) ([]byte, error)

func readKey(path string, read FileReader) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrConfiguration
	}
	body, err := read(path, 66)
	if err != nil {
		return nil, ErrConfiguration
	}
	value := string(body)
	if strings.HasSuffix(value, "\r\n") {
		value = strings.TrimSuffix(value, "\r\n")
	} else {
		value = strings.TrimSuffix(value, "\n")
	}
	if !secretPattern.MatchString(value) {
		return nil, ErrConfiguration
	}
	key, _ := hex.DecodeString(value)
	return key, nil
}
func LoadSources(body []byte, read FileReader) ([]Source, error) {
	fail := func() ([]Source, error) { return nil, ErrConfiguration }
	if len(body) > MaxConfig {
		return fail()
	}
	root, err := object(body, "version", "sources")
	if err != nil {
		return fail()
	}
	var version int
	var entries []json.RawMessage
	if decode(root["version"], &version) != nil || version != 1 || decode(root["sources"], &entries) != nil || len(entries) == 0 || len(entries) > 32 {
		return fail()
	}
	sources := make([]Source, 0, len(entries))
	seen := map[string]bool{}
	for _, entry := range entries {
		fields, err := object(entry, "source_id", "workspace_id", "component_id", "job_full_name", "keys")
		if err != nil {
			return fail()
		}
		var s Source
		var keys []json.RawMessage
		for k, v := range map[string]any{"source_id": &s.ID, "workspace_id": &s.WorkspaceID, "component_id": &s.ComponentID, "job_full_name": &s.Job, "keys": &keys} {
			if decode(fields[k], v) != nil {
				return fail()
			}
		}
		if !tokenPattern.MatchString(s.ID) || seen[s.ID] || !scopedID.MatchString(s.WorkspaceID) || !strings.HasPrefix(s.WorkspaceID, "wrk_") || !scopedID.MatchString(s.ComponentID) || !strings.HasPrefix(s.ComponentID, "cmp_") || !validJob(s.Job) || len(keys) < 1 || len(keys) > 2 {
			return fail()
		}
		seen[s.ID] = true
		s.keys = map[string][]byte{}
		for _, entry := range keys {
			f, err := object(entry, "key_id", "secret_file")
			if err != nil {
				return fail()
			}
			var id, path string
			if decode(f["key_id"], &id) != nil || decode(f["secret_file"], &path) != nil || !tokenPattern.MatchString(id) {
				return fail()
			}
			if _, ok := s.keys[id]; ok {
				return fail()
			}
			key, err := readKey(path, read)
			if err != nil {
				return fail()
			}
			s.keys[id] = key
		}
		sources = append(sources, s)
	}
	return sources, nil
}

type SenderConfig struct {
	Origin   string
	SourceID string
	KeyID    string
	secret   []byte
}

func (s SenderConfig) String() string   { return "Jenkins sender configuration (redacted)" }
func (s SenderConfig) GoString() string { return s.String() }
func LoadSender(body []byte, read FileReader) (SenderConfig, error) {
	var c SenderConfig
	if len(body) > MaxConfig {
		return c, ErrConfiguration
	}
	fields, err := object(body, "version", "origin", "source_id", "key_id", "secret_file")
	if err != nil {
		return c, ErrConfiguration
	}
	var version int
	var path string
	for k, v := range map[string]any{"version": &version, "origin": &c.Origin, "source_id": &c.SourceID, "key_id": &c.KeyID, "secret_file": &path} {
		if decode(fields[k], v) != nil {
			return SenderConfig{}, ErrConfiguration
		}
	}
	if version != 1 || !validSender(c) {
		return SenderConfig{}, ErrConfiguration
	}
	c.secret, err = readKey(path, read)
	if err != nil {
		return SenderConfig{}, ErrConfiguration
	}
	return c, nil
}

func validSender(c SenderConfig) bool {
	u, err := url.Parse(c.Origin)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.Opaque == "" && tokenPattern.MatchString(c.SourceID) && tokenPattern.MatchString(c.KeyID)
}
