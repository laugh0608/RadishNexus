package jenkins

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
)

var ErrSpool = errors.New("spool_io_failed")
var ErrSpoolState = errors.New("invalid_spool_state")
var ErrSpoolCapacity = errors.New("spool_capacity_exceeded")
var ErrSpoolLocked = errors.New("spool_locked")
var ErrSpoolBinding = errors.New("spool_binding_mismatch")

const MaxSpoolEntries = 10000
const MaxSpoolBytes int64 = 512 * 1024 * 1024
const MaxSpoolRecord = 32 * 1024

type SpoolBinding struct {
	Version     int    `json:"version"`
	ReceiverID  string `json:"receiver_id"`
	JenkinsID   string `json:"jenkins_id"`
	Origin      string `json:"origin"`
	SourceID    string `json:"source_id"`
	WorkspaceID string `json:"workspace_id"`
	ComponentID string `json:"component_id"`
	Job         string `json:"job_full_name"`
	FirstBuild  int64  `json:"first_build_number"`
}

type SpoolConfig struct {
	Version    int          `json:"version"`
	SenderFile string       `json:"sender_config_file"`
	InputDir   string       `json:"input_dir"`
	StateDir   string       `json:"state_dir"`
	AckDir     string       `json:"ack_dir"`
	Binding    SpoolBinding `json:"binding"`
}

func parseBinding(raw []byte) (SpoolBinding, error) {
	var b SpoolBinding
	if _, err := object(raw, "version", "receiver_id", "jenkins_id", "origin", "source_id", "workspace_id", "component_id", "job_full_name", "first_build_number"); err != nil || json.Unmarshal(raw, &b) != nil {
		return b, ErrSpoolBinding
	}
	if b.Version != 1 || !tokenPattern.MatchString(b.ReceiverID) || !tokenPattern.MatchString(b.JenkinsID) || !validSender(SenderConfig{Origin: b.Origin, SourceID: b.SourceID, KeyID: "validation"}) || !validJob(b.Job) || b.FirstBuild < 1 || b.FirstBuild > 2147483647 || !scopedID.MatchString(b.WorkspaceID) || !strings.HasPrefix(b.WorkspaceID, "wrk_") || !scopedID.MatchString(b.ComponentID) || !strings.HasPrefix(b.ComponentID, "cmp_") {
		return b, ErrSpoolBinding
	}
	return b, nil
}

func LoadSpoolConfig(raw []byte) (SpoolConfig, error) {
	var c SpoolConfig
	if len(raw) > MaxConfig {
		return c, ErrConfiguration
	}
	f, err := object(raw, "version", "sender_config_file", "input_dir", "state_dir", "ack_dir", "binding")
	if err != nil || json.Unmarshal(raw, &c) != nil || c.Version != 1 {
		return c, ErrConfiguration
	}
	c.Binding, err = parseBinding(f["binding"])
	if err != nil {
		return c, err
	}
	for _, p := range []string{c.SenderFile, c.InputDir, c.StateDir, c.AckDir} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return c, ErrConfiguration
		}
	}
	paths := []string{c.InputDir, c.StateDir, c.AckDir}
	for i, p := range paths {
		if c.SenderFile == p || strings.HasPrefix(c.SenderFile, p+string(filepath.Separator)) {
			return c, ErrConfiguration
		}
		for j, q := range paths {
			if i != j && (p == q || strings.HasPrefix(p, q+string(filepath.Separator))) {
				return c, ErrConfiguration
			}
		}
	}
	return c, nil
}

func (c SpoolConfig) Sender() (SenderConfig, error) {
	raw, err := ReadFile(c.SenderFile, MaxConfig)
	if err != nil {
		return SenderConfig{}, err
	}
	s, err := LoadSender(raw, func(path string, limit int64) ([]byte, error) {
		actual, e := filepath.EvalSymlinks(path)
		if e != nil {
			return nil, ErrConfiguration
		}
		for _, dir := range []string{c.InputDir, c.StateDir, c.AckDir} {
			if actual == dir || strings.HasPrefix(actual, dir+string(filepath.Separator)) {
				return nil, ErrConfiguration
			}
		}
		return ReadFile(path, limit)
	})
	if err != nil {
		return s, err
	}
	if s.Origin != c.Binding.Origin || s.SourceID != c.Binding.SourceID {
		return SenderConfig{}, ErrSpoolBinding
	}
	return s, nil
}
