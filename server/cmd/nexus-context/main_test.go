package main

import (
	"bytes"
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateContextFiles(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "credentials.json")
	if e := writeNew(p, []byte(`{"synthetic":"fixture"}`)); e != nil {
		t.Fatal(e)
	}
	if _, e := readPrivate(p, 1024); e != nil {
		t.Fatal(e)
	}
	if e := writeNew(p, []byte("overwrite")); e == nil {
		t.Fatal("overwrote plan")
	}
	if _, e := readPrivate(p, 1); e == nil {
		t.Fatal("oversized input accepted")
	}
	for _, mode := range []string{"public", "symlink", "hardlink", "directory"} {
		t.Run(mode, func(t *testing.T) {
			q := filepath.Join(dir, mode)
			var e error
			switch mode {
			case "public":
				e = os.WriteFile(q, []byte("synthetic"), 0644)
			case "symlink":
				e = os.Symlink(p, q)
			case "hardlink":
				e = os.Link(p, q)
			case "directory":
				e = os.Mkdir(q, 0700)
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = readPrivate(q, 1024); e == nil {
				t.Fatal("unsafe private input accepted")
			}
		})
	}
	var output bytes.Buffer
	if e := run(context.Background(), []string{"apply", "--password", "do-not-echo"}, &output); e == nil || strings.Contains(e.Error(), "do-not-echo") || output.Len() != 0 {
		t.Fatal("credential argument leaked")
	}
}
func TestContextClientTLSAndRedirectBoundary(t *testing.T) {
	requests := 0
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; w.WriteHeader(200) }))
	defer remote.Close()
	local := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, remote.URL, http.StatusTemporaryRedirect)
	}))
	defer local.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if e := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: local.Certificate().Raw}), 0600); e != nil {
		t.Fatal(e)
	}
	for _, origin := range []string{"http://example.test", "https://example.test/path", "https://user:secret@example.test", "https://example.test?secret=value"} {
		if _, e := newClient(origin, ""); e == nil {
			t.Fatal("non-origin accepted")
		}
	}
	client, e := newClient(local.URL, ca)
	if e != nil {
		t.Fatal(e)
	}
	if e = client.request(context.Background(), "POST", "/api/v1/auth/sessions", map[string]string{"password": "synthetic"}, nil); e == nil || requests != 0 || strings.Contains(e.Error(), "synthetic") {
		t.Fatal("redirect was followed or credentials leaked", e)
	}
	untrusted, e := newClient(local.URL, "")
	if e != nil {
		t.Fatal(e)
	}
	if e = untrusted.request(context.Background(), "GET", "/", nil, nil); e == nil {
		t.Fatal("untrusted TLS accepted")
	}
}
