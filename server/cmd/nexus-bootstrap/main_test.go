package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseOptionsRequiresExplicitPasswordStdin(t *testing.T) {
	t.Parallel()
	_, err := parseOptions([]string{
		"--login", "admin",
		"--display-name", "Admin",
		"--workspace-name", "Workspace",
	})
	if err == nil || !strings.Contains(err.Error(), "--password-stdin is required") {
		t.Fatalf("parseOptions() error = %v", err)
	}
	parsed, err := parseOptions([]string{
		"--login", "admin",
		"--display-name", "Admin",
		"--workspace-name", "Workspace",
		"--password-stdin",
	})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if parsed.loginName != "admin" || parsed.displayName != "Admin" ||
		parsed.workspaceName != "Workspace" || !parsed.passwordStdin {
		t.Fatalf("parseOptions() = %#v", parsed)
	}
}

func TestParseOptionsRejectsPositionalAndPasswordArguments(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--login", "admin", "--display-name", "Admin", "--workspace-name", "Workspace", "--password-stdin", "secret"},
		{"--login", "admin", "--display-name", "Admin", "--workspace-name", "Workspace", "--password", "secret"},
	} {
		if _, err := parseOptions(args); err == nil {
			t.Fatalf("parseOptions(%q) error = nil", args)
		}
	}
}

func TestReadPasswordRemovesOnlyOneLineEnding(t *testing.T) {
	t.Parallel()
	password, err := readPassword(strings.NewReader("  correct horse battery staple  \r\n"))
	if err != nil {
		t.Fatalf("readPassword() error = %v", err)
	}
	if password != "  correct horse battery staple  " {
		t.Fatalf("readPassword() = %q", password)
	}
}

func TestReadPasswordRejectsMultipleLinesAndOversizeInput(t *testing.T) {
	t.Parallel()
	if _, err := readPassword(strings.NewReader("first line\nsecond line\n")); err == nil {
		t.Fatal("readPassword() multiple lines error = nil")
	}
	if _, err := readPassword(bytes.NewReader(bytes.Repeat([]byte{'x'}, maxPasswordInputBytes))); err == nil {
		t.Fatal("readPassword() oversize error = nil")
	}
}
