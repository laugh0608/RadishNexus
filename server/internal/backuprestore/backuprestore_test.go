package backuprestore

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/db"
)

type stubRunner struct {
	output []byte
	err    error
	name   string
	args   []string
}

func (runner *stubRunner) Run(
	_ context.Context,
	name string,
	args []string,
	_ []string,
) ([]byte, error) {
	runner.name = name
	runner.args = slices.Clone(args)
	return slices.Clone(runner.output), runner.err
}

func TestLoadToolMajor(t *testing.T) {
	runner := &stubRunner{output: []byte("pg_dump (PostgreSQL) 17.10\n")}
	service := &Service{runner: runner}

	major, err := service.loadToolMajor(context.Background(), "/tools/pg_dump", "pg_dump")
	if err != nil {
		t.Fatalf("loadToolMajor() error = %v", err)
	}
	if major != 17 || runner.name != "/tools/pg_dump" || !slices.Equal(runner.args, []string{"--version"}) {
		t.Fatalf("loadToolMajor() = %d, runner = %q %#v", major, runner.name, runner.args)
	}
}

func TestLoadToolMajorRejectsUnexpectedOutput(t *testing.T) {
	service := &Service{runner: &stubRunner{output: []byte("not-a-postgres-tool 17.10")}}
	_, err := service.loadToolMajor(context.Background(), "pg_dump", "pg_dump")
	if err == nil {
		t.Fatal("loadToolMajor() error = nil, want unexpected output error")
	}
}

func TestValidateManifestRequiresCurrentContract(t *testing.T) {
	manifest := validTestManifest(t)
	if err := validateManifest(manifest); err != nil {
		t.Fatalf("validateManifest() error = %v", err)
	}

	manifest.Migrations[0].Checksum = strings.Repeat("0", 64)
	if err := validateManifest(manifest); err == nil {
		t.Fatal("validateManifest() migration drift error = nil")
	}

	manifest = validTestManifest(t)
	manifest.ExcludedDataTables = nil
	if err := validateManifest(manifest); err == nil {
		t.Fatal("validateManifest() table classification drift error = nil")
	}
}

func TestManifestRoundTripRejectsUnknownFields(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, manifestFilename)
	manifest := validTestManifest(t)
	if err := writeManifest(path, manifest); err != nil {
		t.Fatalf("writeManifest() error = %v", err)
	}
	loaded, err := readManifest(path)
	if err != nil {
		t.Fatalf("readManifest() error = %v", err)
	}
	if loaded.Format != manifest.Format || loaded.DatabaseDump != manifest.DatabaseDump ||
		!slices.Equal(loaded.Migrations, manifest.Migrations) {
		t.Fatalf("readManifest() = %#v, want %#v", loaded, manifest)
	}

	unknown := []byte(`{"format":"radishnexus-postgresql-backup","unknown":true}`)
	if err := os.WriteFile(path, unknown, 0o600); err != nil {
		t.Fatalf("write unknown manifest: %v", err)
	}
	if _, err := readManifest(path); err == nil {
		t.Fatal("readManifest() unknown field error = nil")
	}
}

func TestBackupOutputMustBeNewChildDirectory(t *testing.T) {
	parent := t.TempDir()
	existing := filepath.Join(parent, "existing")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if _, _, _, err := validateNewOutputDirectory(existing); err == nil {
		t.Fatal("validateNewOutputDirectory() existing path error = nil")
	}

	want := filepath.Join(parent, "new-backup")
	resolved, gotParent, base, err := validateNewOutputDirectory(want)
	if err != nil {
		t.Fatalf("validateNewOutputDirectory() error = %v", err)
	}
	if resolved != want || gotParent != parent || base != "new-backup" {
		t.Fatalf("validateNewOutputDirectory() = %q, %q, %q", resolved, gotParent, base)
	}
}

func TestBackupInputRejectsSymlink(t *testing.T) {
	parent := t.TempDir()
	realDirectory := filepath.Join(parent, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(realDirectory, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	if _, err := validateInputDirectory(link); err == nil {
		t.Fatal("validateInputDirectory() symlink error = nil")
	}
}

func TestDigestRegularFileRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "dump")
	if err := os.WriteFile(file, []byte("backup"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	digest, size, err := digestRegularFile(file)
	if err != nil {
		t.Fatalf("digestRegularFile() error = %v", err)
	}
	if len(digest) != 64 || size != 6 {
		t.Fatalf("digestRegularFile() = %q, %d", digest, size)
	}

	link := filepath.Join(directory, "dump-link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	if _, _, err := digestRegularFile(link); err == nil {
		t.Fatal("digestRegularFile() symlink error = nil")
	}
}

func TestPostgresEnvironmentReplacesConnectionValues(t *testing.T) {
	t.Setenv("PGDATABASE", "old")
	t.Setenv("PGAPPNAME", "old-app")
	environment, err := postgresEnvironment(
		"postgres://backup-user:backup-password@127.0.0.1:5433/backup-db?sslmode=disable",
		"backup-test",
	)
	if err != nil {
		t.Fatalf("postgresEnvironment() error = %v", err)
	}
	var databaseValues int
	var appValues int
	var passwordValues int
	for _, item := range environment {
		switch item {
		case "PGDATABASE=backup-db":
			databaseValues++
		case "PGAPPNAME=backup-test":
			appValues++
		case "PGPASSWORD=backup-password":
			passwordValues++
		case "PGDATABASE=old", "PGAPPNAME=old-app":
			t.Fatalf("postgresEnvironment() retained stale value %q", item)
		}
	}
	if databaseValues != 1 || appValues != 1 || passwordValues != 1 {
		t.Fatalf(
			"postgresEnvironment() connection counts = %d, %d, %d",
			databaseValues,
			appValues,
			passwordValues,
		)
	}
}

func TestPostgresEnvironmentRejectsTLSInsteadOfWeakeningVerification(t *testing.T) {
	_, err := postgresEnvironment(
		"postgres://backup-user:backup-password@database.example/backup-db?sslmode=verify-full",
		"backup-test",
	)
	if err == nil || !strings.Contains(err.Error(), "not forwarded approximately") {
		t.Fatalf("postgresEnvironment() TLS error = %v", err)
	}
}

func TestBuildRestoreListMovesEntityTypesBeforeOtherTableData(t *testing.T) {
	input := []byte(`; archive
1; 0 0 SCHEMA - radishnexus owner
2; 0 0 TABLE radishnexus ci_runs owner
3; 0 0 TABLE DATA radishnexus ci_runs owner
4; 0 0 TABLE DATA public radishnexus_schema_migrations owner
5; 0 0 TABLE DATA radishnexus entity_types owner
6; 0 0 CONSTRAINT radishnexus ci_runs constraint owner
`)
	output, err := buildRestoreList(input)
	if err != nil {
		t.Fatalf("buildRestoreList() error = %v", err)
	}
	text := string(output)
	registry := strings.Index(text, "TABLE DATA radishnexus entity_types")
	ciRuns := strings.Index(text, "TABLE DATA radishnexus ci_runs")
	if registry == -1 || ciRuns == -1 || registry > ciRuns {
		t.Fatalf("buildRestoreList() output =\n%s", text)
	}
}

func TestBuildRestoreListRejectsActivityData(t *testing.T) {
	_, err := buildRestoreList([]byte(`
1; 0 0 TABLE DATA radishnexus entity_types owner
2; 0 0 TABLE DATA radishnexus activity_items owner
`))
	if err == nil || !strings.Contains(err.Error(), "Activity") {
		t.Fatalf("buildRestoreList() error = %v, want Activity exclusion error", err)
	}
}

func validTestManifest(t *testing.T) Manifest {
	t.Helper()
	history, err := db.CurrentMigrationHistory()
	if err != nil {
		t.Fatalf("CurrentMigrationHistory() error = %v", err)
	}
	return Manifest{
		Format:             FormatName,
		FormatVersion:      FormatVersion,
		CreatedAt:          time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC),
		PostgreSQLMajor:    SupportedPostgresMajor,
		PGDumpMajor:        SupportedPostgresMajor,
		Migrations:         slices.Clone(history),
		IncludedDataTables: slices.Clone(includedDataTables),
		ExcludedDataTables: slices.Clone(excludedDataTables),
		DatabaseDump: DumpDescriptor{
			File:      dumpFilename,
			Format:    "postgresql-custom",
			SHA256:    strings.Repeat("a", 64),
			SizeBytes: 42,
		},
	}
}
