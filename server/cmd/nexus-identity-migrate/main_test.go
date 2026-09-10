package main

import (
	"io"
	"strings"
	"testing"
)

func TestMappingInputRejectsSecretsInErrorsAndInvalidShapes(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `[]`, `[{"user_id":"usr_a","email":"private@example.test","password":"private"}]`, `[{"user_id":"usr_a","email":"private@example.test"}] {}`, strings.Repeat("x", 1<<20+1)} {
		if _, err := readMappings(strings.NewReader(body)); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("unsafe mapping error", err)
		}
	}
	if err := run([]string{"--email", "private@example.test"}, strings.NewReader(""), io.Discard); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("unsafe argument error", err)
	}
	if mappings, err := readMappings(strings.NewReader(`[{"user_id":"usr_a","email":"admin@example.test"}]`)); err != nil || len(mappings) != 1 {
		t.Fatal(err)
	}
}
