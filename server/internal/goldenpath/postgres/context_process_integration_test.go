//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
)

type contextProcessConfig struct {
	DSN      string `json:"dsn"`
	Database string `json:"database"`
	Ready    string `json:"ready"`
}
type contextProcessReady struct {
	Origin string `json:"origin"`
	CA     string `json:"ca"`
}

// Child test process hosts the real Session/HTTPS adapters and ContextService.
// Killing it tests OS process death, not just canceling an application context.
func TestContextImportServerProcess(t *testing.T) {
	configPath := os.Getenv("RADISHNEXUS_CONTEXT_PROCESS_CONFIG")
	if configPath == "" {
		t.Skip("subprocess helper")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal("read child config")
	}
	var config contextProcessConfig
	if err = cp.Decode(raw, &config); err != nil {
		t.Fatal("decode child config")
	}
	poolConfig, err := pgxpool.ParseConfig(config.DSN)
	if err != nil {
		t.Fatal("parse child pool")
	}
	poolConfig.ConnConfig.Database = config.Database
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal("connect child pool")
	}
	defer pool.Close()
	auth := authn.NewService(authpostgres.New(pool), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{}, authn.SystemClock{})
	store := goldenpostgres.New(pool)
	f := &contextFixture{pool: pool, auth: auth, store: store, service: goldenpath.NewContextService(goldenpostgres.NewContextStore(pool, contextSchemaReady))}
	f.identity = authn.NewIdentityService(authpostgres.New(pool), auth, "")
	f.configuration = goldenpath.NewConfigurationService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	f.documents = goldenpath.NewDocumentService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	server, ca := contextHTTPS(t, f, "")
	if err = os.WriteFile(config.Ready, cp.Encode(contextProcessReady{server.URL, ca}), 0600); err != nil {
		t.Fatal("write child ready")
	}
	// Parent owns lifetime; timeout is a failure, never a fabricated successful run.
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	<-timer.C
	t.Fatal("child server was not stopped by parent")
}

func contextChildServer(t *testing.T, f *contextFixture) (*exec.Cmd, contextProcessReady) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "process.json")
	readyPath := filepath.Join(dir, "ready.json")
	config := contextProcessConfig{DSN: f.pool.Config().ConnConfig.ConnString(), Database: f.pool.Config().ConnConfig.Database, Ready: readyPath}
	if err := os.WriteFile(configPath, cp.Encode(config), 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(binary, "-test.run=^TestContextImportServerProcess$", "-test.timeout=3m")
	// DSN is a private synthetic fixture file, never a command-line argument/log.
	child.Env = append(os.Environ(), "RADISHNEXUS_CONTEXT_PROCESS_CONFIG="+configPath, "TMPDIR="+dir)
	var output bytes.Buffer
	child.Stdout = &output
	child.Stderr = &output
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(readyPath)
		if err == nil {
			var ready contextProcessReady
			if cp.Decode(raw, &ready) == nil {
				return child, ready
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("child HTTPS startup timed out")
	return nil, contextProcessReady{}
}

func TestContextProcessDeathAndRetry(t *testing.T) {
	binary := os.Getenv("RADISHNEXUS_CONTEXT_CLI")
	if binary == "" {
		t.Skip("RADISHNEXUS_CONTEXT_CLI required")
	}
	source := contextDatabase(t)
	source.seed(t)
	b := source.bundle(t)
	for _, mode := range []string{"kill-cli", "kill-server"} {
		t.Run(mode, func(t *testing.T) {
			target := contextDatabase(t)
			users := contextCount(t, target, "users")
			child, ready := contextChildServer(t, target)
			dir := t.TempDir()
			bundlePath := filepath.Join(dir, "input.nexus")
			planPath := filepath.Join(dir, "plan.json")
			if err := cp.WriteDirectory(bundlePath, b); err != nil {
				t.Fatal(err)
			}
			login := contextCredentialFile(t, "context.owner@example.test")
			common := []string{"--origin", ready.Origin, "--ca", ready.CA, "--credentials-file", login, "--workspace", target.workspace}
			contextCLI(t, append(append([]string{"prepare"}, common...), "--input", bundlePath, "--operation", contextOperation, "--output", planPath)...)
			raw, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			var plan cp.Plan
			if err = cp.Decode(raw, &plan); err != nil {
				t.Fatal(err)
			}
			release := contextGate(t, target, "activity_items")
			args := append(append([]string{"apply"}, common...), "--input", bundlePath, "--plan", planPath, "--confirm-digest", plan.PlanDigest)
			command := exec.Command(binary, args...)
			var output bytes.Buffer
			command.Stdout = &output
			command.Stderr = &output
			if err = command.Start(); err != nil {
				t.Fatal(err)
			}
			var waitErr error
			waited := make(chan struct{})
			go func() { waitErr = command.Wait(); close(waited) }()
			t.Cleanup(func() { _ = command.Process.Kill(); <-waited })
			contextWaitGate(t, target)
			if mode == "kill-cli" {
				if err = command.Process.Kill(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err = child.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				if err = child.Wait(); err == nil {
					t.Fatal("server was not killed")
				}
			}
			select {
			case <-waited:
				if waitErr == nil {
					t.Fatal("interrupted CLI reported success")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("CLI did not exit")
			}
			// Releasing the test gate does not establish rollback. Wait until the dead
			// request's transaction has ended, then query the receipt independently.
			release()
			ctx, cancel := context.WithTimeout(target.ctx, 10*time.Second)
			defer cancel()
			waitProjectionLock(t, ctx, target.pool, `SELECT NOT EXISTS(SELECT 1 FROM pg_locks WHERE database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND locktype='advisory' AND objid=904400)`)
			for _, table := range []string{"teams", "projects", "channels", "messages", "threads", "decisions", "tickets", "documents", "document_revisions", "entity_links", "context_import_batches", "context_import_mappings", "context_import_history", "domain_events", "activity_items", "outbox_deliveries"} {
				if contextCount(t, target, table) != 0 {
					t.Fatal("process death left partial data", table)
				}
			}
			if contextCount(t, target, "users") != users {
				t.Fatal("process death left historical identity")
			}
			result, err := target.service.Result(target.ctx, target.principal, target.owner.Token, plan.OperationID)
			if err != nil || result.Status != "not_observed" {
				t.Fatal("process death receipt", err)
			}
			if mode == "kill-server" {
				_, ready = contextChildServer(t, target)
				common = []string{"--origin", ready.Origin, "--ca", ready.CA, "--credentials-file", login, "--workspace", target.workspace}
				args = append(append([]string{"apply"}, common...), "--input", bundlePath, "--plan", planPath, "--confirm-digest", plan.PlanDigest)
			}
			contextCLI(t, args...)
			resultRaw := contextCLI(t, append(append([]string{"result"}, common...), "--operation", contextOperation)...)
			if err = cp.Decode(resultRaw, &result); err != nil || result.Status != "applied" {
				t.Fatal("retry did not apply", err)
			}
			contextCLI(t, args...)
			if contextCount(t, target, "context_import_batches") != 1 || contextCount(t, target, "messages") != 1 || contextCount(t, target, "outbox_deliveries") != 0 {
				t.Fatal("retry duplicated graph/delivery")
			}
			t.Log("SIGKILL at Activity insertion: transaction ended, empty target and not_observed verified; same-plan CLI retry and result confirmed one applied graph")
		})
	}
}
