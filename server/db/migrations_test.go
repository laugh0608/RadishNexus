package db

import (
	"testing"
	"testing/fstest"
)

func TestLoadMigrationsRequiresContinuousSequence(t *testing.T) {
	t.Parallel()

	_, err := loadMigrations(fstest.MapFS{
		"migrations/001_first.sql": {Data: []byte("SELECT 1;")},
		"migrations/003_third.sql": {Data: []byte("SELECT 3;")},
	})
	if err == nil {
		t.Fatal("loadMigrations() error = nil, want sequence gap error")
	}
}

func TestLoadMigrationsUsesOnlyForwardSQLAndHashesArtifact(t *testing.T) {
	t.Parallel()

	loaded, err := loadMigrations(fstest.MapFS{
		"migrations/001_first.sql": {Data: []byte("SELECT 1;\n" + downMarker + "\nSELECT 2;")},
	})
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}
	if len(loaded) != 1 || loaded[0].sequence != 1 || loaded[0].name != "first" {
		t.Fatalf("loadMigrations() = %#v", loaded)
	}
	if loaded[0].upSQL != "SELECT 1;" {
		t.Fatalf("upSQL = %q", loaded[0].upSQL)
	}
	if len(loaded[0].checksum) != 64 {
		t.Fatalf("checksum length = %d", len(loaded[0].checksum))
	}
}
