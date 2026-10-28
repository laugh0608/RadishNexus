package contextpackage

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRecordAndLineLimits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int
	}{
		{"objects", MaxObjects}, {"actors", MaxObjects}, {"history", MaxHistory}, {"links", MaxHistory}, {"revisions", MaxHistory},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, count := range []int{tc.limit - 1, tc.limit, tc.limit + 1} {
				var records []struct {
					ID int `json:"id"`
				}
				raw := bytes.Repeat([]byte("{\"id\":1}\n"), count)
				err := readLines(raw, count, &records, tc.limit)
				if (err == nil) != (count <= tc.limit) {
					t.Fatalf("count %d: %v", count, err)
				}
			}
		})
	}
	for _, size := range []int{MaxLineBytes - 1, MaxLineBytes, MaxLineBytes + 1} {
		raw := append([]byte(`{"id":1}`+strings.Repeat(" ", size-8)), '\n')
		var records []struct {
			ID int `json:"id"`
		}
		err := readLines(raw, 1, &records, 1)
		if (err == nil) != (size <= MaxLineBytes) {
			t.Fatalf("line bytes %d: %v", size, err)
		}
	}
}

func TestValidateRejectsEveryRecordOverflow(t *testing.T) {
	for name, change := range map[string]func(*Data){
		"objects":   func(d *Data) { d.Objects = make([]Object, MaxObjects+1) },
		"actors":    func(d *Data) { d.Actors = make([]Actor, MaxObjects+1) },
		"history":   func(d *Data) { d.History = make([]History, MaxHistory+1) },
		"links":     func(d *Data) { d.Links = make([]Link, MaxHistory+1) },
		"revisions": func(d *Data) { d.Revisions = make([]Revision, MaxHistory+1) },
	} {
		t.Run(name, func(t *testing.T) {
			d, _, err := Unpack(fixture(t))
			if err != nil {
				t.Fatal(err)
			}
			change(&d)
			if _, err = Validate(d); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "record limits") {
				t.Fatalf("overflow: %v", err)
			}
		})
	}
}

func TestManifestAndDocumentByteLimits(t *testing.T) {
	b := fixture(t)
	for _, size := range []int{MaxLineBytes - 1, MaxLineBytes, MaxLineBytes + 1} {
		dir := filepath.Join(t.TempDir(), "input.nexus")
		if err := WriteDirectory(dir, b); err != nil {
			t.Fatal(err)
		}
		raw := Encode(b.Manifest)
		raw = append(raw, bytes.Repeat([]byte(" "), size-len(raw))...)
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		_, err := ReadDirectory(dir)
		if (err == nil) != (size <= MaxLineBytes) {
			t.Fatalf("manifest %d: %v", size, err)
		}
	}
	for _, size := range []int{262143, 262144, 262145} {
		d, _, err := Unpack(b)
		if err != nil {
			t.Fatal(err)
		}
		d.Bodies[BodyPath("doc_a", 1)] = strings.Repeat("x", size)
		_, err = Validate(d)
		if (err == nil) != (size <= 262144) {
			t.Fatalf("body %d: %v", size, err)
		}
	}
}

// Build a semantically valid near-limit package with continuous document history.
// This is parser input, not a claim of full database capacity or native creation.
func fullSizeBundle(t *testing.T) Bundle {
	t.Helper()
	b := fixture(t)
	d, _, err := Unpack(b)
	if err != nil {
		t.Fatal(err)
	}
	at := b.Manifest.CreatedAt
	for n := 3; n <= 256; n++ {
		when := at.Add(time.Duration(n) * time.Second)
		event := fmt.Sprintf("evt_revision_%d", n)
		d.History = append(d.History, History{ID: event, Type: "document.revised", Version: 1, Entity: Ref{"document", "doc_a"}, Actor: "usr_a", Source: "web", OccurredAt: when, CorrelationID: "cor_a", Facts: Encode(Facts{Revision: n}), Lineage: []Origin{}})
		d.Revisions = append(d.Revisions, Revision{DocumentID: "doc_a", Revision: n, Title: "Capacity", Format: "nexus-markdown-v1", Actor: "usr_a", CreatedAt: when, EventID: event, BodyFile: BodyPath("doc_a", n)})
	}
	for _, r := range d.Revisions {
		d.Bodies[r.BodyFile] = strings.Repeat("x", 262144)
	}
	d.Objects[7].Data = Encode(Document{Revision: 256, Visibility: "project"})
	d.Objects[7].UpdatedAt = at.Add(256 * time.Second)
	b.Manifest.CreatedAt = d.Objects[7].UpdatedAt
	// Leave room for metadata, then pad a JSONL record with insignificant spaces.
	d.Bodies[BodyPath("doc_a", 256)] = "x"
	b, err = Pack(b.Manifest, d)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		total := len(Encode(b.Manifest))
		for _, f := range b.Files {
			total += len(f.Content)
		}
		delta := MaxBytes - total
		if delta == 0 {
			return b
		}
		for j := range b.Files {
			if b.Files[j].Path == "objects.jsonl" {
				raw := b.Files[j].Content
				if delta > 0 {
					raw = append(append(raw[:len(raw)-1:len(raw)-1], bytes.Repeat([]byte(" "), delta)...), '\n')
				} else {
					raw = append(raw[:len(raw)-1+delta:len(raw)-1+delta], '\n')
				}
				b.Files[j].Content = raw
				for k := range b.Manifest.Files {
					if b.Manifest.Files[k].Path == "objects.jsonl" {
						b.Manifest.Files[k].Bytes = int64(len(raw))
						b.Manifest.Files[k].SHA256 = Hash(raw)
					}
				}
			}
		}
	}
	t.Fatal("size did not converge")
	return Bundle{}
}

func TestPackageExactTotalByteLimit(t *testing.T) {
	b := fullSizeBundle(t)
	start := time.Now()
	var memoryBefore, memoryAfter runtime.MemStats
	runtime.ReadMemStats(&memoryBefore)
	if _, _, err := Unpack(b); err != nil {
		t.Fatal("exact limit rejected", err)
	}
	dir := filepath.Join(t.TempDir(), "full.nexus")
	if err := WriteDirectory(dir, b); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDirectory(dir); err != nil {
		t.Fatal("writer produced unreadable limit package", err)
	}
	runtime.ReadMemStats(&memoryAfter)
	t.Logf("64 MiB valid bundle, 256 revisions: unpack/write/read=%s total_alloc_delta=%d heap_in_use_after=%d (Go heap sample, not process peak RSS)", time.Since(start), memoryAfter.TotalAlloc-memoryBefore.TotalAlloc, memoryAfter.HeapInuse)
	for i := range b.Files {
		if b.Files[i].Path == "objects.jsonl" {
			b.Files[i].Content = append(b.Files[i].Content, ' ')
		}
	}
	if _, _, err := Unpack(b); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "package size") {
		t.Fatalf("over limit: %v", err)
	}
}
