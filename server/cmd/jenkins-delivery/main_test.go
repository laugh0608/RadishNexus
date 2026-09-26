package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandRejectsUnsafeArgumentsWithoutPrintingValues(t *testing.T) {
	for _, args := range [][]string{nil, {"-secret", "sensitive"}, {"-config", "relative", "-input", "relative"}, {"-config", "/missing-sensitive", "-input", "/missing-sensitive"}} {
		var out bytes.Buffer
		err := run(args, &out)
		if err == nil || out.Len() != 0 || strings.Contains(err.Error(), "sensitive") {
			t.Fatal(err, out.String())
		}
	}
}
func TestCommandChecksInputBeforeNetwork(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	config := filepath.Join(dir, "sender.json")
	input := filepath.Join(dir, "input.json")
	for path, body := range map[string]string{key: strings.Repeat("ab", 32), config: `{"version":1,"origin":"https://nexus.invalid","source_id":"source_a","key_id":"key_a","secret_file":"` + key + `"}`, input: `{"version":1}`} {
		if e := os.WriteFile(path, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	var out bytes.Buffer
	e := run([]string{"-config", config, "-input", input}, &out)
	if e == nil || e.Error() != "invalid_request" || out.Len() != 0 {
		t.Fatal(e)
	}
}
