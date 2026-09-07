package runtimeconfig

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestDatabaseURLKeepsExistingConfigurationWithoutPasswordFile(t *testing.T) {
	t.Parallel()
	want := "host=localhost user=radishnexus dbname=radishnexus"
	got, err := DatabaseURL(environment(map[string]string{databaseURLKey: want}), unreadable)
	if err != nil {
		t.Fatalf("DatabaseURL() error = %v", err)
	}
	if got != want {
		t.Fatalf("DatabaseURL() = %q, want %q", got, want)
	}
}

func TestDatabaseURLOverlaysPasswordFromSecretFile(t *testing.T) {
	t.Parallel()
	got, err := DatabaseURL(
		environment(map[string]string{
			databaseURLKey:          "postgres://radishnexus@postgres:5432/radishnexus?sslmode=disable",
			databasePasswordFileKey: "/run/secrets/postgres_password",
		}),
		func(path string) ([]byte, error) {
			if path != "/run/secrets/postgres_password" {
				t.Fatalf("read path = %q", path)
			}
			return []byte("correct:/ password?#\r\n"), nil
		},
	)
	if err != nil {
		t.Fatalf("DatabaseURL() error = %v", err)
	}
	config, err := pgx.ParseConfig(got)
	if err != nil {
		t.Fatalf("parse resolved URL: %v", err)
	}
	if config.Password != "correct:/ password?#" {
		t.Fatalf("resolved password = %q", config.Password)
	}
}

func TestDatabaseURLRejectsAmbiguousOrInvalidSecretConfiguration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		values      map[string]string
		secret      []byte
		wantMessage string
	}{
		{name: "missing URL", values: map[string]string{}, wantMessage: "DATABASE_URL is required"},
		{
			name: "relative secret path",
			values: map[string]string{
				databaseURLKey:          "postgres://radishnexus@postgres/radishnexus",
				databasePasswordFileKey: "secret",
			},
			wantMessage: "absolute path",
		},
		{
			name: "non URL connection string",
			values: map[string]string{
				databaseURLKey:          "host=postgres user=radishnexus",
				databasePasswordFileKey: "/run/secrets/password",
			},
			secret:      []byte("password"),
			wantMessage: "PostgreSQL URL",
		},
		{
			name: "missing user",
			values: map[string]string{
				databaseURLKey:          "postgres://postgres/radishnexus",
				databasePasswordFileKey: "/run/secrets/password",
			},
			secret:      []byte("password"),
			wantMessage: "include a database user",
		},
		{
			name: "embedded password",
			values: map[string]string{
				databaseURLKey:          "postgres://radishnexus:embedded@postgres/radishnexus",
				databasePasswordFileKey: "/run/secrets/password",
			},
			secret:      []byte("password"),
			wantMessage: "must not embed a password",
		},
		{
			name: "empty password",
			values: map[string]string{
				databaseURLKey:          "postgres://radishnexus@postgres/radishnexus",
				databasePasswordFileKey: "/run/secrets/password",
			},
			wantMessage: "is empty",
		},
		{
			name: "multiple lines",
			values: map[string]string{
				databaseURLKey:          "postgres://radishnexus@postgres/radishnexus",
				databasePasswordFileKey: "/run/secrets/password",
			},
			secret:      []byte("first\nsecond\n"),
			wantMessage: "exactly one",
		},
		{
			name: "oversize",
			values: map[string]string{
				databaseURLKey:          "postgres://radishnexus@postgres/radishnexus",
				databasePasswordFileKey: "/run/secrets/password",
			},
			secret:      []byte(strings.Repeat("x", maxDatabasePasswordSize+1)),
			wantMessage: "exceeds",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := DatabaseURL(environment(test.values), func(string) ([]byte, error) {
				return test.secret, nil
			})
			if err == nil || !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("DatabaseURL() error = %v, want %q", err, test.wantMessage)
			}
		})
	}
}

func TestDatabaseURLPreservesReadFailureWithoutSecretContent(t *testing.T) {
	t.Parallel()
	_, err := DatabaseURL(
		environment(map[string]string{
			databaseURLKey:          "postgres://radishnexus@postgres/radishnexus",
			databasePasswordFileKey: "/run/secrets/password",
		}),
		func(string) ([]byte, error) { return nil, errors.New("permission denied") },
	)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("DatabaseURL() error = %v", err)
	}
}

func environment(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func unreadable(string) ([]byte, error) {
	return nil, errors.New("unexpected read")
}
