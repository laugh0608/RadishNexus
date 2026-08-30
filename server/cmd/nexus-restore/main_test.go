package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestRunRequiresInputBeforeDatabase(t *testing.T) {
	err := run(context.Background(), nil, func(string) string { return "" }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--input") {
		t.Fatalf("run() error = %v, want input error", err)
	}
}

func TestRunRequiresDatabaseURL(t *testing.T) {
	err := run(
		context.Background(),
		[]string{"--input", "backup"},
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
		[]string{"--input", "backup", "extra"},
		func(string) string { return "ignored" },
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "positional") {
		t.Fatalf("run() error = %v, want positional argument error", err)
	}
}
