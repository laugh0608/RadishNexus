// Package backuprestore implements the first explicit PostgreSQL backup and
// restore boundary for the Golden Path. It deliberately produces a database
// backup, not the future portable .nexus export format.
package backuprestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/laugh0608/RadishNexus/server/db"
)

const (
	FormatName              = "radishnexus-postgresql-backup"
	FormatVersion           = 1
	SupportedPostgresMajor  = 17
	manifestFilename        = "manifest.json"
	dumpFilename            = "database.dump"
	maxManifestBytes        = 1 << 20
	defaultPGDumpExecutable = "pg_dump"
	defaultPGRestoreTool    = "pg_restore"
)

var (
	includedDataTables = []string{
		"public.radishnexus_schema_migrations",
		"radishnexus.channel_memberships",
		"radishnexus.channels",
		"radishnexus.ci_runs",
		"radishnexus.components",
		"radishnexus.decisions",
		"radishnexus.deployments",
		"radishnexus.domain_events",
		"radishnexus.entity_links",
		"radishnexus.entity_types",
		"radishnexus.environment_deployment_authorizations",
		"radishnexus.environments",
		"radishnexus.inbound_deliveries",
		"radishnexus.local_accounts",
		"radishnexus.messages",
		"radishnexus.outbox_deliveries",
		"radishnexus.project_memberships",
		"radishnexus.projects",
		"radishnexus.relation_types",
		"radishnexus.teams",
		"radishnexus.thread_memberships",
		"radishnexus.threads",
		"radishnexus.tickets",
		"radishnexus.users",
		"radishnexus.workspace_memberships",
		"radishnexus.workspaces",
	}
	excludedDataTables = []string{
		"radishnexus.activity_items",
		"radishnexus.user_sessions",
	}
	toolVersionPattern = regexp.MustCompile(`\b([0-9]+)(?:\.[0-9]+)+\b`)
)

type DumpDescriptor struct {
	File      string `json:"file"`
	Format    string `json:"format"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

type Manifest struct {
	Format             string               `json:"format"`
	FormatVersion      int                  `json:"format_version"`
	CreatedAt          time.Time            `json:"created_at"`
	PostgreSQLMajor    int                  `json:"postgresql_major"`
	PGDumpMajor        int                  `json:"pg_dump_major"`
	Migrations         []db.MigrationRecord `json:"migrations"`
	IncludedDataTables []string             `json:"included_data_tables"`
	ExcludedDataTables []string             `json:"excluded_data_tables"`
	DatabaseDump       DumpDescriptor       `json:"database_dump"`
}

type ToolRunner interface {
	Run(context.Context, string, []string, []string) ([]byte, error)
}

type ExecToolRunner struct{}

func (ExecToolRunner) Run(
	ctx context.Context,
	name string,
	args []string,
	environment []string,
) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = environment
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(output.String())
		if detail == "" {
			return nil, fmt.Errorf("run %s: %w", filepath.Base(name), err)
		}
		return nil, fmt.Errorf("run %s: %w: %s", filepath.Base(name), err, detail)
	}
	return output.Bytes(), nil
}

type Service struct {
	runner    ToolRunner
	now       func() time.Time
	pgDump    string
	pgRestore string
}

func NewService() *Service {
	pgDump := os.Getenv("RADISHNEXUS_PG_DUMP")
	if pgDump == "" {
		pgDump = defaultPGDumpExecutable
	}
	pgRestore := os.Getenv("RADISHNEXUS_PG_RESTORE")
	if pgRestore == "" {
		pgRestore = defaultPGRestoreTool
	}
	return &Service{
		runner:    ExecToolRunner{},
		now:       time.Now,
		pgDump:    pgDump,
		pgRestore: pgRestore,
	}
}

func (service *Service) Backup(
	ctx context.Context,
	connection *pgx.Conn,
	databaseURL string,
	outputDirectory string,
) (manifest Manifest, err error) {
	if connection == nil {
		return Manifest{}, errors.New("backup connection is required")
	}
	if databaseURL == "" {
		return Manifest{}, errors.New("backup database URL is required")
	}
	finalPath, parent, base, err := validateNewOutputDirectory(outputDirectory)
	if err != nil {
		return Manifest{}, err
	}

	serverMajor, err := loadServerMajor(ctx, connection)
	if err != nil {
		return Manifest{}, err
	}
	if serverMajor != SupportedPostgresMajor {
		return Manifest{}, fmt.Errorf(
			"PostgreSQL major %d is unsupported by backup format version %d; require %d",
			serverMajor,
			FormatVersion,
			SupportedPostgresMajor,
		)
	}
	history, err := validateCurrentMigrationHistory(ctx, connection)
	if err != nil {
		return Manifest{}, err
	}
	if err := validateDatabaseTableInventory(ctx, connection); err != nil {
		return Manifest{}, err
	}
	pgDumpMajor, err := service.loadToolMajor(ctx, service.pgDump, "pg_dump")
	if err != nil {
		return Manifest{}, err
	}
	if pgDumpMajor != SupportedPostgresMajor {
		return Manifest{}, fmt.Errorf(
			"pg_dump major %d is unsupported by backup format version %d; require %d",
			pgDumpMajor,
			FormatVersion,
			SupportedPostgresMajor,
		)
	}
	toolEnvironment, err := postgresEnvironment(databaseURL, "radishnexus-backup")
	if err != nil {
		return Manifest{}, err
	}

	temporaryPath, err := os.MkdirTemp(parent, "."+base+".partial-")
	if err != nil {
		return Manifest{}, fmt.Errorf("create temporary backup directory: %w", err)
	}
	defer func() {
		if err != nil {
			if cleanupErr := os.RemoveAll(temporaryPath); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("remove incomplete backup directory: %w", cleanupErr))
			}
		}
	}()
	if chmodErr := os.Chmod(temporaryPath, 0o700); chmodErr != nil {
		return Manifest{}, fmt.Errorf("restrict temporary backup directory: %w", chmodErr)
	}

	dumpPath := filepath.Join(temporaryPath, dumpFilename)
	args := []string{
		"--format=custom",
		"--file=" + dumpPath,
		"--no-owner",
		"--no-privileges",
		"--no-comments",
		"--no-large-objects",
		"--no-publications",
		"--no-security-labels",
		"--no-subscriptions",
		"--no-tablespaces",
	}
	for _, table := range excludedDataTables {
		args = append(args, "--exclude-table-data="+table)
	}
	if _, err := service.runner.Run(
		ctx,
		service.pgDump,
		args,
		toolEnvironment,
	); err != nil {
		return Manifest{}, fmt.Errorf("create PostgreSQL dump: %w", err)
	}
	if err := os.Chmod(dumpPath, 0o600); err != nil {
		return Manifest{}, fmt.Errorf("restrict PostgreSQL dump permissions: %w", err)
	}
	dumpDigest, dumpSize, err := digestRegularFile(dumpPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("digest PostgreSQL dump: %w", err)
	}
	if dumpSize == 0 {
		return Manifest{}, errors.New("PostgreSQL dump is empty")
	}

	manifest = Manifest{
		Format:             FormatName,
		FormatVersion:      FormatVersion,
		CreatedAt:          service.now().UTC(),
		PostgreSQLMajor:    serverMajor,
		PGDumpMajor:        pgDumpMajor,
		Migrations:         slices.Clone(history),
		IncludedDataTables: slices.Clone(includedDataTables),
		ExcludedDataTables: slices.Clone(excludedDataTables),
		DatabaseDump: DumpDescriptor{
			File:      dumpFilename,
			Format:    "postgresql-custom",
			SHA256:    dumpDigest,
			SizeBytes: dumpSize,
		},
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, fmt.Errorf("validate generated backup manifest: %w", err)
	}
	if err := writeManifest(filepath.Join(temporaryPath, manifestFilename), manifest); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return Manifest{}, fmt.Errorf("publish completed backup directory: %w", err)
	}
	return manifest, nil
}

func (service *Service) Restore(
	ctx context.Context,
	connection *pgx.Conn,
	databaseURL string,
	inputDirectory string,
) (Manifest, error) {
	if connection == nil {
		return Manifest{}, errors.New("restore connection is required")
	}
	if databaseURL == "" {
		return Manifest{}, errors.New("restore database URL is required")
	}
	inputPath, err := validateInputDirectory(inputDirectory)
	if err != nil {
		return Manifest{}, err
	}
	manifest, err := readManifest(filepath.Join(inputPath, manifestFilename))
	if err != nil {
		return Manifest{}, err
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, fmt.Errorf("validate backup manifest: %w", err)
	}
	dumpPath := filepath.Join(inputPath, manifest.DatabaseDump.File)
	digest, size, err := digestRegularFile(dumpPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("read PostgreSQL dump: %w", err)
	}
	if digest != manifest.DatabaseDump.SHA256 || size != manifest.DatabaseDump.SizeBytes {
		return Manifest{}, errors.New("PostgreSQL dump checksum or size does not match manifest")
	}

	pgRestoreMajor, err := service.loadToolMajor(ctx, service.pgRestore, "pg_restore")
	if err != nil {
		return Manifest{}, err
	}
	if pgRestoreMajor != manifest.PostgreSQLMajor {
		return Manifest{}, fmt.Errorf(
			"pg_restore major %d does not match backup PostgreSQL major %d",
			pgRestoreMajor,
			manifest.PostgreSQLMajor,
		)
	}
	inspectEnvironment, err := postgresEnvironment(databaseURL, "radishnexus-restore-inspect")
	if err != nil {
		return Manifest{}, err
	}
	tableOfContents, err := service.runner.Run(
		ctx,
		service.pgRestore,
		[]string{"--list", dumpPath},
		inspectEnvironment,
	)
	if err != nil {
		return Manifest{}, fmt.Errorf("inspect PostgreSQL dump: %w", err)
	}
	restoreList, err := buildRestoreList(tableOfContents)
	if err != nil {
		return Manifest{}, fmt.Errorf("prepare PostgreSQL restore order: %w", err)
	}
	restoreListFile, err := os.CreateTemp("", "radishnexus-restore-list-*.toc")
	if err != nil {
		return Manifest{}, fmt.Errorf("create temporary PostgreSQL restore list: %w", err)
	}
	restoreListPath := restoreListFile.Name()
	defer func() {
		_ = os.Remove(restoreListPath)
	}()
	if err := restoreListFile.Chmod(0o600); err != nil {
		restoreListFile.Close()
		return Manifest{}, fmt.Errorf("restrict temporary PostgreSQL restore list: %w", err)
	}
	if _, err := restoreListFile.Write(restoreList); err != nil {
		restoreListFile.Close()
		return Manifest{}, fmt.Errorf("write temporary PostgreSQL restore list: %w", err)
	}
	if err := restoreListFile.Close(); err != nil {
		return Manifest{}, fmt.Errorf("close temporary PostgreSQL restore list: %w", err)
	}

	targetMajor, err := loadServerMajor(ctx, connection)
	if err != nil {
		return Manifest{}, err
	}
	if targetMajor != manifest.PostgreSQLMajor {
		return Manifest{}, fmt.Errorf(
			"target PostgreSQL major %d does not match backup major %d",
			targetMajor,
			manifest.PostgreSQLMajor,
		)
	}
	if err := requireEmptyTarget(ctx, connection); err != nil {
		return Manifest{}, err
	}
	restoreEnvironment, err := postgresEnvironment(databaseURL, "radishnexus-restore")
	if err != nil {
		return Manifest{}, err
	}
	if _, err := service.runner.Run(
		ctx,
		service.pgRestore,
		[]string{
			"--dbname=",
			"--single-transaction",
			"--exit-on-error",
			"--no-owner",
			"--no-privileges",
			"--no-comments",
			"--no-publications",
			"--no-security-labels",
			"--no-subscriptions",
			"--no-tablespaces",
			"--use-list=" + restoreListPath,
			dumpPath,
		},
		restoreEnvironment,
	); err != nil {
		return Manifest{}, fmt.Errorf("restore PostgreSQL dump: %w", err)
	}
	if err := db.Migrate(ctx, connection); err != nil {
		return Manifest{}, fmt.Errorf("validate restored migration history and migrate forward: %w", err)
	}
	if _, err := validateCurrentMigrationHistory(ctx, connection); err != nil {
		return Manifest{}, err
	}
	if err := validateDatabaseTableInventory(ctx, connection); err != nil {
		return Manifest{}, err
	}
	var activityCount int64
	if err := connection.QueryRow(ctx, `SELECT count(*) FROM radishnexus.activity_items`).Scan(&activityCount); err != nil {
		return Manifest{}, fmt.Errorf("read restored Activity projection count: %w", err)
	}
	if activityCount != 0 {
		return Manifest{}, fmt.Errorf("restored Activity projection contains %d rows; want zero before rebuild", activityCount)
	}
	return manifest, nil
}

func (service *Service) loadToolMajor(
	ctx context.Context,
	executable string,
	wantTool string,
) (int, error) {
	output, err := service.runner.Run(ctx, executable, []string{"--version"}, os.Environ())
	if err != nil {
		return 0, fmt.Errorf("read %s version: %w", wantTool, err)
	}
	text := strings.TrimSpace(string(output))
	if !strings.Contains(text, wantTool) {
		return 0, fmt.Errorf("unexpected %s version output %q", wantTool, text)
	}
	matches := toolVersionPattern.FindStringSubmatch(text)
	if matches == nil {
		return 0, fmt.Errorf("parse %s version output %q", wantTool, text)
	}
	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("parse %s major version: %w", wantTool, err)
	}
	return major, nil
}

func buildRestoreList(tableOfContents []byte) ([]byte, error) {
	lines := strings.Split(strings.TrimSuffix(string(tableOfContents), "\n"), "\n")
	registryIndex := -1
	firstTableDataIndex := -1
	for index, line := range lines {
		schema, table, ok := restoreListTableData(line)
		if !ok {
			continue
		}
		if firstTableDataIndex == -1 {
			firstTableDataIndex = index
		}
		if schema == "radishnexus" && table == "activity_items" {
			return nil, errors.New("backup unexpectedly contains derived Activity table data")
		}
		if schema == "radishnexus" && table == "entity_types" {
			if registryIndex != -1 {
				return nil, errors.New("backup contains duplicate EntityType registry data entries")
			}
			registryIndex = index
		}
	}
	if registryIndex == -1 || firstTableDataIndex == -1 {
		return nil, errors.New("backup does not contain required EntityType registry data")
	}
	registryLine := lines[registryIndex]
	lines = append(lines[:registryIndex], lines[registryIndex+1:]...)
	if registryIndex < firstTableDataIndex {
		firstTableDataIndex--
	}
	lines = append(lines, "")
	copy(lines[firstTableDataIndex+1:], lines[firstTableDataIndex:])
	lines[firstTableDataIndex] = registryLine
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func restoreListTableData(line string) (schema string, table string, ok bool) {
	fields := strings.Fields(line)
	for index := 0; index+3 < len(fields); index++ {
		if fields[index] == "TABLE" && fields[index+1] == "DATA" {
			return fields[index+2], fields[index+3], true
		}
	}
	return "", "", false
}

func validateNewOutputDirectory(path string) (finalPath string, parent string, base string, err error) {
	if strings.TrimSpace(path) == "" {
		return "", "", "", errors.New("backup output directory is required")
	}
	finalPath, err = filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", "", "", fmt.Errorf("resolve backup output directory: %w", err)
	}
	if _, statErr := os.Lstat(finalPath); statErr == nil {
		return "", "", "", fmt.Errorf("backup output already exists: %s", finalPath)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", "", "", fmt.Errorf("inspect backup output: %w", statErr)
	}
	parent = filepath.Dir(finalPath)
	info, err := os.Stat(parent)
	if err != nil {
		return "", "", "", fmt.Errorf("inspect backup output parent: %w", err)
	}
	if !info.IsDir() {
		return "", "", "", fmt.Errorf("backup output parent is not a directory: %s", parent)
	}
	base = filepath.Base(finalPath)
	if base == "." || base == string(filepath.Separator) {
		return "", "", "", errors.New("backup output must name a new child directory")
	}
	return finalPath, parent, base, nil
}

func validateInputDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("backup input directory is required")
	}
	resolved, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve backup input directory: %w", err)
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect backup input directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("backup input must be a real directory: %s", resolved)
	}
	return resolved, nil
}

func loadServerMajor(ctx context.Context, connection *pgx.Conn) (int, error) {
	var raw string
	if err := connection.QueryRow(ctx, `SHOW server_version_num`).Scan(&raw); err != nil {
		return 0, fmt.Errorf("read PostgreSQL server version: %w", err)
	}
	versionNumber, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse PostgreSQL server version %q: %w", raw, err)
	}
	return versionNumber / 10000, nil
}

func validateCurrentMigrationHistory(
	ctx context.Context,
	connection *pgx.Conn,
) ([]db.MigrationRecord, error) {
	expected, err := db.CurrentMigrationHistory()
	if err != nil {
		return nil, fmt.Errorf("load embedded migration history: %w", err)
	}
	rows, err := connection.Query(ctx, `
		SELECT sequence, name, checksum
		FROM public.radishnexus_schema_migrations
		ORDER BY sequence
	`)
	if err != nil {
		return nil, fmt.Errorf("read source migration history: %w", err)
	}
	defer rows.Close()

	actual := make([]db.MigrationRecord, 0, len(expected))
	for rows.Next() {
		var record db.MigrationRecord
		if err := rows.Scan(&record.Sequence, &record.Name, &record.Checksum); err != nil {
			return nil, fmt.Errorf("scan source migration history: %w", err)
		}
		actual = append(actual, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source migration history: %w", err)
	}
	if !slices.Equal(actual, expected) {
		return nil, fmt.Errorf("database migration history does not exactly match the current binary")
	}
	return actual, nil
}

func validateDatabaseTableInventory(ctx context.Context, connection *pgx.Conn) error {
	var largeObjectCount int64
	if err := connection.QueryRow(ctx, `SELECT count(*) FROM pg_catalog.pg_largeobject_metadata`).Scan(&largeObjectCount); err != nil {
		return fmt.Errorf("read database large-object inventory: %w", err)
	}
	if largeObjectCount != 0 {
		return fmt.Errorf("database contains %d unclassified large objects", largeObjectCount)
	}

	extensionRows, err := connection.Query(ctx, `
		SELECT extname
		FROM pg_catalog.pg_extension
		ORDER BY extname
	`)
	if err != nil {
		return fmt.Errorf("read database extension inventory: %w", err)
	}
	var extensions []string
	for extensionRows.Next() {
		var extension string
		if err := extensionRows.Scan(&extension); err != nil {
			extensionRows.Close()
			return fmt.Errorf("scan database extension inventory: %w", err)
		}
		extensions = append(extensions, extension)
	}
	if err := extensionRows.Err(); err != nil {
		extensionRows.Close()
		return fmt.Errorf("iterate database extension inventory: %w", err)
	}
	extensionRows.Close()
	if !slices.Equal(extensions, []string{"plpgsql"}) {
		return fmt.Errorf(
			"database extension inventory is not supported for backup: got %v, want [plpgsql]",
			extensions,
		)
	}

	schemaRows, err := connection.Query(ctx, `
		SELECT nspname
		FROM pg_catalog.pg_namespace
		WHERE nspname NOT LIKE 'pg_%'
		  AND nspname <> 'information_schema'
		ORDER BY nspname
	`)
	if err != nil {
		return fmt.Errorf("read database schema inventory: %w", err)
	}
	var schemas []string
	for schemaRows.Next() {
		var schema string
		if err := schemaRows.Scan(&schema); err != nil {
			schemaRows.Close()
			return fmt.Errorf("scan database schema inventory: %w", err)
		}
		schemas = append(schemas, schema)
	}
	if err := schemaRows.Err(); err != nil {
		schemaRows.Close()
		return fmt.Errorf("iterate database schema inventory: %w", err)
	}
	schemaRows.Close()
	if !slices.Equal(schemas, []string{"public", "radishnexus"}) {
		return fmt.Errorf(
			"database schema inventory is not supported for backup: got %v, want [public radishnexus]",
			schemas,
		)
	}

	rows, err := connection.Query(ctx, `
		SELECT namespace.nspname || '.' || class.relname
		FROM pg_catalog.pg_class AS class
		JOIN pg_catalog.pg_namespace AS namespace
		  ON namespace.oid = class.relnamespace
		WHERE namespace.nspname NOT LIKE 'pg_%'
		  AND namespace.nspname <> 'information_schema'
		  AND class.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
		ORDER BY namespace.nspname, class.relname
	`)
	if err != nil {
		return fmt.Errorf("read database relation inventory: %w", err)
	}
	defer rows.Close()

	actual := make([]string, 0, len(includedDataTables)+len(excludedDataTables))
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return fmt.Errorf("scan database relation inventory: %w", err)
		}
		actual = append(actual, table)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate database relation inventory: %w", err)
	}
	expected := append(slices.Clone(includedDataTables), excludedDataTables...)
	sort.Strings(expected)
	if !slices.Equal(actual, expected) {
		return fmt.Errorf(
			"database relation inventory is not fully classified for backup: got %v, want %v",
			actual,
			expected,
		)
	}
	return nil
}

func requireEmptyTarget(ctx context.Context, connection *pgx.Conn) error {
	var occupied bool
	if err := connection.QueryRow(ctx, `
		SELECT
			EXISTS (
				SELECT 1
				FROM pg_catalog.pg_namespace
				WHERE nspname NOT LIKE 'pg_%'
				  AND nspname NOT IN ('public', 'information_schema')
			)
			OR EXISTS (
				SELECT 1
				FROM pg_catalog.pg_class AS class
				JOIN pg_catalog.pg_namespace AS namespace
				  ON namespace.oid = class.relnamespace
				WHERE namespace.nspname = 'public'
				  AND class.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
			)
	`).Scan(&occupied); err != nil {
		return fmt.Errorf("inspect restore target: %w", err)
	}
	if occupied {
		return errors.New("restore target is not empty; automatic overwrite and cleanup are forbidden")
	}
	return nil
}

func validateManifest(manifest Manifest) error {
	if manifest.Format != FormatName || manifest.FormatVersion != FormatVersion {
		return fmt.Errorf("unsupported backup format %q version %d", manifest.Format, manifest.FormatVersion)
	}
	if manifest.CreatedAt.IsZero() || manifest.CreatedAt.Location() != time.UTC {
		return errors.New("backup creation time must be a non-zero UTC timestamp")
	}
	if manifest.PostgreSQLMajor != SupportedPostgresMajor || manifest.PGDumpMajor != SupportedPostgresMajor {
		return fmt.Errorf("backup requires PostgreSQL and pg_dump major %d", SupportedPostgresMajor)
	}
	expectedMigrations, err := db.CurrentMigrationHistory()
	if err != nil {
		return fmt.Errorf("load embedded migration history: %w", err)
	}
	if !slices.Equal(manifest.Migrations, expectedMigrations) {
		return errors.New("backup migration history does not match the current binary")
	}
	if !slices.Equal(manifest.IncludedDataTables, includedDataTables) ||
		!slices.Equal(manifest.ExcludedDataTables, excludedDataTables) {
		return errors.New("backup table classification does not match the current binary")
	}
	if manifest.DatabaseDump.File != dumpFilename ||
		manifest.DatabaseDump.Format != "postgresql-custom" ||
		manifest.DatabaseDump.SizeBytes <= 0 {
		return errors.New("backup dump descriptor is invalid")
	}
	if len(manifest.DatabaseDump.SHA256) != 64 {
		return errors.New("backup dump SHA-256 is invalid")
	}
	if _, err := hex.DecodeString(manifest.DatabaseDump.SHA256); err != nil {
		return errors.New("backup dump SHA-256 is invalid")
	}
	return nil
}

func writeManifest(path string, manifest Manifest) error {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode backup manifest: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("write backup manifest: %w", err)
	}
	return nil
}

func readManifest(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open backup manifest: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Manifest{}, fmt.Errorf("inspect backup manifest: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxManifestBytes {
		return Manifest{}, errors.New("backup manifest must be a small regular file")
	}

	decoder := json.NewDecoder(io.LimitReader(file, maxManifestBytes+1))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode backup manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Manifest{}, errors.New("backup manifest contains trailing JSON data")
	}
	return manifest, nil
}

func digestRegularFile(path string) (digest string, size int64, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", 0, errors.New("file is not a regular non-symlink file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	if written != info.Size() {
		return "", 0, errors.New("file size changed while hashing")
	}
	return hex.EncodeToString(hash.Sum(nil)), written, nil
}

func postgresEnvironment(databaseURL string, applicationName string) ([]string, error) {
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL for PostgreSQL tools: %w", err)
	}
	if config.Host == "" || config.Database == "" || config.User == "" {
		return nil, errors.New("database URL must include host, database, and user for PostgreSQL tools")
	}
	if config.TLSConfig != nil {
		return nil, errors.New(
			"PostgreSQL tool bridge currently requires an explicit sslmode=disable connection; TLS settings are not forwarded approximately",
		)
	}

	environment := make([]string, 0, len(os.Environ())+8)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "PGHOST=") ||
			strings.HasPrefix(item, "PGPORT=") ||
			strings.HasPrefix(item, "PGDATABASE=") ||
			strings.HasPrefix(item, "PGUSER=") ||
			strings.HasPrefix(item, "PGPASSWORD=") ||
			strings.HasPrefix(item, "PGSSLMODE=") ||
			strings.HasPrefix(item, "PGAPPNAME=") {
			continue
		}
		environment = append(environment, item)
	}
	return append(
		environment,
		"PGHOST="+config.Host,
		"PGPORT="+strconv.Itoa(int(config.Port)),
		"PGDATABASE="+config.Database,
		"PGUSER="+config.User,
		"PGPASSWORD="+config.Password,
		"PGSSLMODE=disable",
		"PGAPPNAME="+applicationName,
	), nil
}
