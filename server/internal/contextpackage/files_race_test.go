package contextpackage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadRejectsReplacementBetweenStatAndOpen(t *testing.T) {
	for _, kind := range []string{"regular", "symlink", "hardlink", "fifo", "directory"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			name := filepath.Join(dir, "input")
			if err := os.WriteFile(name, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			before, err := root.Lstat("input")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Rename(name, filepath.Join(dir, "original")); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "regular":
				err = os.WriteFile(name, []byte("replacement"), 0600)
			case "symlink":
				err = os.Symlink("original", name)
			case "hardlink":
				err = os.Link(filepath.Join(dir, "original"), name)
			case "fifo":
				err = makeFIFO(name)
			case "directory":
				err = os.Mkdir(name, 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, e := readRegularSnapshot(root, "input", 1024, before); done <- e }()
			select {
			case err = <-done:
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("accepted %s replacement: %v", kind, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("replacement blocked open")
			}
		})
	}
}

func TestOpenedRootDoesNotFollowDirectoryReplacement(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "package")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "input"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = os.Rename(dir, filepath.Join(parent, "old")); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "input"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	content, err := readRegular(root, "input", 1024)
	if err != nil || string(content) != "original" {
		t.Fatal("root followed replacement", err)
	}
}
