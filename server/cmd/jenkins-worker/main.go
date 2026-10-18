// jenkins-worker durably forwards controller-authored snapshots. It never
// contacts Jenkins, executes repository code, or modifies core business tables.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/jenkins"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, jenkins.SafeSpoolError(err))
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return jenkins.ErrConfiguration
	}
	command := args[0]
	f := flag.NewFlagSet("jenkins-worker", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	path := f.String("config", "", "absolute worker configuration file")
	build := f.Int64("build", 0, "exact build number for status, retry or cleanup")
	collector := f.String("collector-state", "", "absolute read-only collector state directory for cleanup")
	confirmed := f.Bool("confirmed", false, "explicitly apply the selected retry or cleanup")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 || *path == "" || *build < 0 || *build > 2147483647 {
		return jenkins.ErrConfiguration
	}
	if command != "init" && command != "run" && command != "status" && command != "retry" && command != "cleanup-plan" && command != "cleanup" {
		return jenkins.ErrConfiguration
	}
	if (command == "retry" || command == "cleanup") && (*build == 0 || !*confirmed) {
		return jenkins.ErrConfiguration
	}
	if command != "retry" && command != "cleanup" && *confirmed {
		return jenkins.ErrConfiguration
	}
	if command != "retry" && command != "status" && command != "cleanup" && command != "cleanup-plan" && *build != 0 {
		return jenkins.ErrConfiguration
	}
	if (*collector != "" && command != "cleanup" && command != "cleanup-plan") || (command == "cleanup" && *collector == "") || (command == "cleanup-plan" && ((*collector == "") != (*build == 0))) {
		return jenkins.ErrConfiguration
	}
	// macOS runs filesystem unit tests but is not a supported deployed worker.
	if (command == "run" || command == "cleanup") && runtime.GOOS != "linux" {
		return jenkins.ErrConfiguration
	}
	raw, e := jenkins.ReadFile(*path, jenkins.MaxConfig)
	if e != nil {
		return e
	}
	c, e := jenkins.LoadSpoolConfig(raw)
	if e != nil {
		return e
	}
	mode := "run"
	if command == "status" || command == "cleanup-plan" {
		mode = "inspect"
	}
	if command == "init" {
		mode = "init"
	}
	s, e := jenkins.OpenSpool(c, mode)
	if e != nil {
		return e
	}
	defer s.Close()
	encode := func(v any) error {
		if json.NewEncoder(out).Encode(v) != nil {
			return jenkins.ErrSpool
		}
		return nil
	}
	switch command {
	case "init":
		return encode(map[string]string{"state": "initialized", "source_id": c.Binding.SourceID})
	case "status", "cleanup-plan":
		if command == "cleanup-plan" && *collector != "" {
			plan, e := s.CleanupPlan(*build, *collector)
			if e != nil {
				return e
			}
			return encode(plan)
		}
		status, e := s.Status(*build)
		if e != nil {
			return e
		}
		if e = encode(status); e != nil {
			return e
		}
		if status.SourcePaused {
			return jenkins.ErrSpoolPaused
		}
		return nil
	case "cleanup":
		plan, e := s.Cleanup(*build, *collector)
		if e != nil {
			return e
		}
		return encode(map[string]any{"source_id": c.Binding.SourceID, "build_number": *build, "state": "compacted", "already_compacted": plan.AlreadyCompacted})
	case "retry":
		// Recheck current credentials and origin before re-enabling this source.
		if _, e = c.Sender(); e != nil {
			return e
		}
		if e = s.Retry(*build); e != nil {
			return e
		}
		return encode(map[string]any{"state": "retry_scheduled", "source_id": c.Binding.SourceID, "build_number": *build})
	default:
		sender, e := c.Sender()
		if e != nil {
			return e
		}
		if e = encode(map[string]string{"state": "worker_started", "source_id": c.Binding.SourceID}); e != nil {
			return e
		}
		lastStatus := ""
		for {
			worked, tickErr := s.Tick(ctx, sender, nil)
			if tickErr != nil && !errors.Is(tickErr, jenkins.ErrSpoolPaused) {
				return tickErr
			}
			status, e := s.Status(0)
			if e != nil {
				return e
			}
			fingerprint := status
			fingerprint.ObservedAt = time.Time{}
			raw, _ := json.Marshal(fingerprint)
			if string(raw) != lastStatus {
				if e = encode(status); e != nil {
					return e
				}
				lastStatus = string(raw)
			}
			if tickErr != nil {
				return tickErr
			}
			if worked {
				continue
			}
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}
