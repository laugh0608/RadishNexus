//go:build integration

package postgres_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func TestEnvironmentRevocationSerializesWithDeployment(t *testing.T) {
	for _, recordFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("record-first-%v", recordFirst), func(t *testing.T) {
			ctx, pool, core, build := stagingDatabase(t)
			config := goldenpath.NewConfigurationService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
			input := stagingInput(build("succeeded"))
			if _, err := pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET role='owner' WHERE workspace_id='wrk_main' AND user_id='usr_admin'`); err != nil {
				t.Fatal(err)
			}
			// Pause the first formal command after it holds its permission locks.
			// The trigger is only a deterministic test barrier in this disposable DB.
			table := "workspace_configuration_audit"
			if recordFirst {
				table = "deployments"
			}
			if _, err := pool.Exec(ctx, `CREATE FUNCTION radishnexus.test_delivery_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(32,1); RETURN NEW; END $$; CREATE TRIGGER test_delivery_barrier BEFORE INSERT ON radishnexus.`+table+` FOR EACH ROW EXECUTE FUNCTION radishnexus.test_delivery_barrier()`); err != nil {
				t.Fatal(err)
			}
			gate, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer gate.Rollback(ctx)
			if _, err = gate.Exec(ctx, `SELECT pg_advisory_xact_lock(32,1)`); err != nil {
				t.Fatal(err)
			}
			waitForLock := func(pattern string) {
				t.Helper()
				deadline := time.Now().Add(5 * time.Second)
				for {
					var waiting bool
					if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE $1)`, pattern).Scan(&waiting); err != nil {
						t.Fatal(err)
					}
					if waiting {
						return
					}
					if time.Now().After(deadline) {
						t.Fatal("formal command did not reach lock barrier", pattern)
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			recordResult, revokeResult := make(chan error, 1), make(chan error, 1)
			record := func() {
				_, err := core.RecordStagingDeployment(ctx, invocation(principal("usr_contributor"), "serial-record"), input)
				recordResult <- err
			}
			revoke := func() {
				_, err := config.Configure(ctx, invocation(principal("usr_admin"), "serial-revoke"), goldenpath.ConfigurationInput{
					Kind: "environment.authorization.revoke", ScopeID: "env_staging", UserID: "usr_contributor", ClientOperationID: "serial-revoke",
					Delivery: &goldenpath.DeliveryConfigurationInput{Confirmed: true, ExpectedAuthorization: &goldenpath.ConfigurationAuthorization{ID: "dpa_staging_contributor", Status: "active"}},
				})
				revokeResult <- err
			}
			if recordFirst {
				go record()
			} else {
				go revoke()
			}
			waitForLock("%INSERT INTO radishnexus." + table + "%")
			if recordFirst {
				go revoke()
			} else {
				go record()
			}
			waitForLock("%FROM radishnexus.environments%")
			if err = gate.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err = <-revokeResult; err != nil {
				t.Fatal("formal revocation failed", err)
			}
			err = <-recordResult
			if recordFirst && err != nil {
				t.Fatal("authorized first record failed", err)
			}
			if !recordFirst && !errors.Is(err, authz.ErrForbidden) {
				t.Fatal("record crossed completed revocation", err)
			}
			want := 0
			if recordFirst {
				want = 1
			}
			assertTableCount(t, ctx, pool, "radishnexus.deployments", want)
			if _, err = core.RecordStagingDeployment(ctx, invocation(principal("usr_contributor"), "revoked-retry"), input); !errors.Is(err, authz.ErrForbidden) {
				t.Fatal("receipt bypassed current revocation", err)
			}
		})
	}
}
