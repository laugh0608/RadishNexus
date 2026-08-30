package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestRunRequiresOutputBeforeDatabase(t *testing.T) {
	err := run(context.Background(), nil, func(string) string { return "" }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--output") {
		t.Fatalf("run() error = %v, want output error", err)
	}
}

func TestRunRequiresDatabaseURL(t *testing.T) {
	err := run(
		context.Background(),
		[]string{"--output", "backup"},
		func(string) string { return "" },
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("run() error = %v, want DATABASE_URL error", err)
	}
}

func TestRunRejectsPositionalArguments(t *testing.T) {
	err := run(
		context.Background(),
		[]string{"--output", "backup", "extra"},
		func(string) string { return "ignored" },
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "positional") {
		t.Fatalf("run() error = %v, want positional argument error", err)
	}
}
