package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laugh0608/RadishNexus/server/internal/jenkins"
)

func TestWorkerRejectsArgumentsWithoutLeakingValues(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown", "private"}, {"run", "-secret", "private"}, {"init", "-config", "private"}, {"retry", "-config", "/private-sensitive", "-build", "1"}, {"status", "-config", "/private-sensitive", "-confirmed"}, {"status", "-config", "/private-sensitive", "-build", "-1"}} {
		var out bytes.Buffer
		e := run(context.Background(), args, &out)
		if e == nil || out.Len() != 0 || strings.Contains(jenkins.SafeSpoolError(e), "private") {
			t.Fatal(e, out.String())
		}
	}
}

func TestWorkerOfflineInitInspectionAndNoDestructiveCleanup(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	c := jenkins.SpoolConfig{Version: 1, SenderFile: filepath.Join(dir, "missing-secret-config.json"), InputDir: filepath.Join(dir, "inputs"), StateDir: filepath.Join(dir, "state"), AckDir: filepath.Join(dir, "acks"), Binding: jenkins.SpoolBinding{Version: 1, ReceiverID: "receiver", JenkinsID: "jenkins", Origin: "https://nexus.invalid", SourceID: "source", WorkspaceID: "wrk_test", ComponentID: "cmp_test", Job: "example/build", FirstBuild: 1}}
	if e = os.Mkdir(c.InputDir, 0700); e != nil {
		t.Fatal(e)
	}
	config := filepath.Join(dir, "worker.json")
	raw, _ := json.Marshal(c)
	if e = os.WriteFile(config, raw, 0600); e != nil {
		t.Fatal(e)
	}
	for _, command := range []string{"init", "status", "cleanup-plan"} {
		var out bytes.Buffer
		if e = run(context.Background(), []string{command, "-config", config}, &out); e != nil {
			t.Fatal(command, e)
		}
		if strings.Contains(out.String(), dir) || strings.Contains(out.String(), "https:") {
			t.Fatal("configuration leaked")
		}
		if command != "init" && !strings.Contains(out.String(), `"cleanup_enabled":false`) {
			t.Fatal(out.String())
		}
	}
	var out bytes.Buffer
	if e = run(context.Background(), []string{"cleanup", "-config", config}, &out); e == nil {
		t.Fatal("deletion enabled")
	}
	if _, e = os.Stat(filepath.Join(c.StateDir, "manifest.json")); e != nil {
		t.Fatal(e)
	}
}
