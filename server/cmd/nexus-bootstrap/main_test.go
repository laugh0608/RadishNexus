package main

import (
	"strings"
	"testing"
)

func TestBootstrapRequiresPrivateCredentialInput(t *testing.T) {
	for _, args := range [][]string{
		{"--display-name", "Admin", "--workspace-name", "Workspace"},
		{"--login", "private", "--credentials-stdin"},
		{"--password", "private", "--credentials-stdin"},
		{"--credentials-stdin", "private"},
	} {
		if _, err := parseOptions(args); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatalf("unsafe option error: %v", err)
		}
	}
	if _, err := parseOptions([]string{"--display-name", "Admin", "--workspace-name", "Workspace", "--credentials-stdin"}); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialInputIsBoundedStrictAndPreservesPassword(t *testing.T) {
	input, err := readCredentials(strings.NewReader(`{"email":"admin@example.test","password":"  correct horse battery staple  "}`))
	if err != nil || input.Password != "  correct horse battery staple  " {
		t.Fatal("password changed", err)
	}
	for _, body := range []string{`{`, `{}`, `null`, `{"email":"private","password":"private","extra":1}`, `{"email":"private","password":"private"} {}`, strings.Repeat("x", maxCredentialInputBytes+1)} {
		if _, err := readCredentials(strings.NewReader(body)); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("unsafe credential error", err)
		}
	}
}
