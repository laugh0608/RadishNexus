//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

// The parent fixture bootstraps an empty Workspace and accepts an invitation.
// All Repository facts here, including re-linking, use the formal command.
func assertRepositoryConfigurationFromBootstrap(t *testing.T, ctx context.Context, pool *pgxpool.Pool, owner, member authz.Principal, team string) {
	t.Helper()
	store := goldenpostgres.New(pool)
	s := goldenpath.NewConfigurationService(store, goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	call := func(p authz.Principal, in goldenpath.ConfigurationInput) (goldenpath.ConfigurationResult, error) {
		return s.Configure(ctx, goldenpath.Invocation{Principal: p, SourceKind: "web", CorrelationID: "repository-configuration"}, in)
	}
	must := func(in goldenpath.ConfigurationInput) goldenpath.ConfigurationResult {
		t.Helper()
		r, e := call(owner, in)
		if e != nil {
			t.Fatal(in.Kind, e)
		}
		return r
	}
	component := must(goldenpath.ConfigurationInput{Kind: "component.create", ScopeID: owner.WorkspaceID, ClientOperationID: "repository-component", Name: "Repository Service", Key: "repository-service", OwnerTeamID: team, Delivery: &goldenpath.DeliveryConfigurationInput{Type: "service"}}).Object
	create := goldenpath.ConfigurationInput{Kind: "repository.create", ScopeID: owner.WorkspaceID, ClientOperationID: "repo-create", Name: "Service source", Repository: &goldenpath.RepositoryConfigurationInput{RepositoryMetadata: goldenpath.RepositoryMetadata{Provider: "gitea", ProviderOrigin: "https://git.example.test", ExternalID: "00123", WebURL: "https://git.example.test/team/service", DefaultBranch: "main"}}}
	if _, e := call(member, create); !errors.Is(e, authz.ErrForbidden) {
		t.Fatal("ordinary member created mapping", e)
	}
	repo := must(create).Object
	if retry := must(create); retry.Created || retry.Object.ID != repo.ID {
		t.Fatal("mapping exact retry", retry)
	}
	other := create
	other.ClientOperationID = "repo-duplicate"
	if _, e := call(owner, other); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("duplicate identity accepted", e)
	}
	other = create
	other.Name = "Changed name"
	if _, e := call(owner, other); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("changed creation receipt accepted", e)
	}
	read, e := s.ReadConfiguration(ctx, member, "repository", repo.ID)
	if e != nil || read.Repository.ExternalID != "00123" {
		t.Fatal("member metadata read", e)
	}
	link := goldenpath.ConfigurationInput{Kind: "component.repository.link", ScopeID: component.ID, ClientOperationID: "repo-link", Repository: &goldenpath.RepositoryConfigurationInput{RepositoryID: repo.ID, Confirmed: true}}
	if _, e := call(member, link); !errors.Is(e, authz.ErrForbidden) {
		t.Fatal("member linked", e)
	}
	if _, e := call(authz.Principal{Kind: "plugin", ID: "plugin_source", WorkspaceID: owner.WorkspaceID}, link); e == nil {
		t.Fatal("plugin linked")
	}
	first := must(link)
	if !first.Created || first.LinkID == "" {
		t.Fatal("first relation result", first)
	}
	assertPair := func(p authz.Principal, want string) {
		t.Helper()
		for _, q := range []goldenpath.ConfigurationQuery{{Kind: "component-repositories", ScopeID: component.ID, Limit: 25}, {Kind: "repository-components", ScopeID: repo.ID, Limit: 25}} {
			page, e := s.ListConfiguration(ctx, p, q)
			if e != nil {
				t.Fatal("double discovery", e)
			}
			if want == "" {
				if len(page.Links) != 0 {
					t.Fatal("removed link visible", page)
				}
			} else if len(page.Links) != 1 || page.Links[0].ID != want || page.Links[0].CanUnlink != (p.ID == owner.ID) {
				t.Fatal("relation projection", page)
			}
		}
	}
	assertPair(member, first.LinkID)
	for _, ref := range []entityref.Ref{{Type: "component", ID: component.ID}, {Type: "repository", ID: repo.ID}} {
		relations, e := store.ListRelations(ctx, member, ref)
		if e != nil || len(relations) != 1 || relations[0].RelationType != "source-repository" {
			t.Fatal("unified relation projection", relations, e)
		}
		if ref.Type == "repository" && relations[0].Direction != "incoming" {
			t.Fatal("missing reverse direction")
		}
	}
	if r := must(link); r.Created || r.LinkID != first.LinkID {
		t.Fatal("link exact retry", r)
	}
	other = link
	other.ClientOperationID = "duplicate-link"
	if _, e := call(owner, other); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("second active link", e)
	}
	unlink := goldenpath.ConfigurationInput{Kind: "component.repository.unlink", ScopeID: component.ID, ClientOperationID: "repo-unlink", Repository: &goldenpath.RepositoryConfigurationInput{LinkID: first.LinkID, Confirmed: true}}
	if r := must(unlink); r.Created || r.LinkID != first.LinkID {
		t.Fatal("unlink response", r)
	}
	assertPair(member, "")
	must(link)
	assertPair(member, "") // old link receipt cannot reactivate
	other = unlink
	other.ClientOperationID = "stale-unlink"
	if _, e := call(owner, other); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("removed relation accepted as fresh unlink", e)
	}
	link2 := link
	link2.ClientOperationID = "repo-relink"
	second := must(link2)
	if second.LinkID == first.LinkID {
		t.Fatal("relink rewrote history")
	}
	must(unlink)
	assertPair(owner, second.LinkID) // old unlink receipt cannot remove the new link
	var beforeID, removedID, state string
	if e := pool.QueryRow(ctx, `SELECT created_by_id,removed_by_id,state FROM radishnexus.entity_links WHERE id=$1`, first.LinkID).Scan(&beforeID, &removedID, &state); e != nil || beforeID != owner.ID || removedID != owner.ID || state != "removed" {
		t.Fatal("provenance", e)
	}
	for _, sql := range []string{`UPDATE radishnexus.repositories SET default_branch='other' WHERE id=$1`, `DELETE FROM radishnexus.repositories WHERE id=$1`} {
		if _, e := pool.Exec(ctx, sql, repo.ID); e == nil {
			t.Fatal("mutable Repository")
		}
	}
	for _, sql := range []string{`UPDATE radishnexus.entity_links SET state='active',removed_at=NULL,removed_by_id=NULL,removed_by_kind=NULL,removal_reason=NULL WHERE id=$1`, `DELETE FROM radishnexus.entity_links WHERE id=$1`, `UPDATE radishnexus.entity_links SET metadata='{"secret":"no"}' WHERE id=$1`} {
		if _, e := pool.Exec(ctx, sql, first.LinkID); e == nil {
			t.Fatal("mutable relation history")
		}
	}
	// Workspace and Project-role setup below only creates isolation conditions;
	// Repository facts still go through the same formal command.
	if _, e := pool.Exec(ctx, `WITH workspace AS (INSERT INTO radishnexus.workspaces(id,name) VALUES('wrk_repository_foreign','Foreign') RETURNING id) INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,role,status) SELECT w.id,m.user_id,'owner','active' FROM workspace w CROSS JOIN radishnexus.workspace_memberships m WHERE m.workspace_id=$1 AND m.user_id=$2`, owner.WorkspaceID, owner.ID); e != nil {
		t.Fatal(e)
	}
	foreignOwner := owner
	foreignOwner.WorkspaceID = "wrk_repository_foreign"
	foreignCreate := create
	foreignCreate.ScopeID = foreignOwner.WorkspaceID
	foreignRepository, e := call(foreignOwner, foreignCreate)
	if e != nil {
		t.Fatal("formal foreign mapping", e)
	}
	foreignLink := link
	foreignLink.ClientOperationID = "foreign-link"
	foreignLink.Repository = &goldenpath.RepositoryConfigurationInput{RepositoryID: foreignRepository.Object.ID, Confirmed: true}
	if _, e := call(owner, foreignLink); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("cross-workspace link", e)
	}
	if _, e := s.ReadConfiguration(ctx, owner, "repository", foreignRepository.Object.ID); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("cross-workspace metadata", e)
	}
	if _, e := s.ListConfiguration(ctx, foreignOwner, goldenpath.ConfigurationQuery{Kind: "component-repositories", ScopeID: component.ID, Limit: 25}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("cross-workspace reverse discovery", e)
	}
	if _, e := pool.Exec(ctx, `UPDATE radishnexus.project_memberships SET role='admin' WHERE workspace_id=$1 AND user_id=$2`, owner.WorkspaceID, member.ID); e != nil {
		t.Fatal(e)
	}
	for _, input := range []goldenpath.ConfigurationInput{create, link, unlink} {
		if _, e := call(member, input); !errors.Is(e, authz.ErrForbidden) {
			t.Fatal("Project admin gained owner configuration", input.Kind, e)
		}
	}
	// Different actors racing to map one identity or associate one pair must
	// produce one success and one conflict, independent of receipt scope.
	if _, e := pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, owner.WorkspaceID, member.ID); e != nil {
		t.Fatal(e)
	}
	concurrent := func(in goldenpath.ConfigurationInput) goldenpath.ConfigurationResult {
		t.Helper()
		type outcome struct {
			r goldenpath.ConfigurationResult
			e error
		}
		done := make(chan outcome, 2)
		start := make(chan struct{})
		for _, p := range []authz.Principal{owner, member} {
			go func() { <-start; r, e := call(p, in); done <- outcome{r, e} }()
		}
		close(start)
		var result goldenpath.ConfigurationResult
		success, conflicts := 0, 0
		for range 2 {
			o := <-done
			if o.e == nil {
				success++
				result = o.r
			} else if errors.Is(o.e, authz.ErrConflict) {
				conflicts++
			} else {
				t.Fatal("concurrent command", o.e)
			}
		}
		if success != 1 || conflicts != 1 {
			t.Fatal("concurrent outcomes", success, conflicts)
		}
		return result
	}
	other = create
	other.ClientOperationID = "concurrent-create"
	metadata := *create.Repository
	metadata.ExternalID = "456"
	other.Repository = &metadata
	concurrentRepo := concurrent(other).Object
	other = link
	other.ClientOperationID = "concurrent-link"
	other.Repository = &goldenpath.RepositoryConfigurationInput{RepositoryID: concurrentRepo.ID, Confirmed: true}
	concurrent(other)
	if _, e := pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET role='member' WHERE workspace_id=$1 AND user_id=$2`, owner.WorkspaceID, member.ID); e != nil {
		t.Fatal(e)
	}
	// Same repository can be mapped to another Component without copying it.
	component2 := must(goldenpath.ConfigurationInput{Kind: "component.create", ScopeID: owner.WorkspaceID, ClientOperationID: "repo-component-2", Name: "Second Component", Key: "repository-second", OwnerTeamID: team, Delivery: &goldenpath.DeliveryConfigurationInput{Type: "library"}}).Object
	other = link
	other.ScopeID = component2.ID
	other.ClientOperationID = "second-component"
	must(other)
	page, e := s.ListConfiguration(ctx, member, goldenpath.ConfigurationQuery{Kind: "repositories", ScopeID: owner.WorkspaceID, Limit: 1})
	if e != nil || len(page.Objects) != 1 || page.NextID != page.Objects[0].ID {
		t.Fatal("Repository pagination", page, e)
	}
	next, e := s.ListConfiguration(ctx, member, goldenpath.ConfigurationQuery{Kind: "repositories", ScopeID: owner.WorkspaceID, Limit: 1, AfterID: page.NextID})
	if e != nil || len(next.Objects) != 1 || next.Objects[0].ID <= page.Objects[0].ID || next.NextID != "" {
		t.Fatal("Repository next page", next, e)
	}
	for _, q := range []goldenpath.ConfigurationQuery{{Kind: "component-repositories", ScopeID: component.ID, Limit: 1}, {Kind: "repository-components", ScopeID: repo.ID, Limit: 1}} {
		p, e := s.ListConfiguration(ctx, member, q)
		if e != nil || len(p.Links) != 1 || p.NextID == "" {
			t.Fatal("relation first page", p, e)
		}
		q.AfterID = p.NextID
		p2, e := s.ListConfiguration(ctx, member, q)
		if e != nil || len(p2.Links) != 1 || p2.Links[0].Target.ID <= p.Links[0].Target.ID || p2.NextID != "" {
			t.Fatal("relation next page", p2, e)
		}
	}
	// Direct SQL is fault injection only; no product mapping/link is pre-seeded.
	for _, table := range []string{"workspace_configuration_audit", "workspace_configuration_receipts", "domain_events", "outbox_deliveries", "activity_items"} {
		name := "test_repository_rollback"
		if _, e := pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE radishnexus.%s ADD CONSTRAINT %s CHECK(false) NOT VALID`, table, name)); e != nil {
			t.Fatal(e)
		}
		failed := create
		failed.ClientOperationID = "failure-" + table
		r := *create.Repository
		r.ExternalID = "failure-" + table
		failed.Repository = &r
		if _, e := call(owner, failed); e == nil {
			t.Fatal("injected failure swallowed", table)
		}
		failedUnlink := goldenpath.ConfigurationInput{Kind: "component.repository.unlink", ScopeID: component.ID, ClientOperationID: "unlink-failure-" + table, Repository: &goldenpath.RepositoryConfigurationInput{LinkID: second.LinkID, Confirmed: true}}
		if _, e := call(owner, failedUnlink); e == nil {
			t.Fatal("injected unlink failure swallowed", table)
		}
		var relationState string
		if e := pool.QueryRow(ctx, `SELECT state FROM radishnexus.entity_links WHERE id=$1`, second.LinkID).Scan(&relationState); e != nil || relationState != "active" {
			t.Fatal("partial unlink commit", table, relationState, e)
		}
		var count int
		if e := pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.repositories WHERE external_id=$1`, r.ExternalID).Scan(&count); e != nil || count != 0 {
			t.Fatal("partial Repository commit", table, count, e)
		}
		if e := pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.workspace_configuration_receipts WHERE client_operation_id=$1`, failed.ClientOperationID).Scan(&count); e != nil || count != 0 {
			t.Fatal("partial receipt", e)
		}
		if _, e := pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE radishnexus.%s DROP CONSTRAINT %s`, table, name)); e != nil {
			t.Fatal(e)
		}
	}
	// Projection rebuild preserves the same normal-write evidence, including removal.
	var before, after string
	const snapshot = `SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY event_id),'[]'::jsonb)::text FROM radishnexus.activity_items a`
	if e := pool.QueryRow(ctx, snapshot).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e := store.RebuildActivityProjection(ctx); e != nil {
		t.Fatal(e)
	}
	if e := pool.QueryRow(ctx, snapshot).Scan(&after); e != nil || before != after {
		t.Fatal("Repository Activity rebuild drift", e)
	}
	if strings.Contains(after, "git.example.test") || strings.Contains(after, "00123") {
		t.Fatal("metadata leaked into Activity")
	}
	// Non-active assets allow removal, but never new associations or old link receipts.
	if _, e := pool.Exec(ctx, `UPDATE radishnexus.components SET lifecycle='retired' WHERE id=$1`, component2.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := call(owner, other); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("retired component accepted link retry", e)
	}
	links, e := s.ListConfiguration(ctx, owner, goldenpath.ConfigurationQuery{Kind: "component-repositories", ScopeID: component2.ID, Limit: 25})
	if e != nil {
		t.Fatal(e)
	}
	must(goldenpath.ConfigurationInput{Kind: "component.repository.unlink", ScopeID: component2.ID, ClientOperationID: "retired-unlink", Repository: &goldenpath.RepositoryConfigurationInput{LinkID: links.Links[0].ID, Confirmed: true}})
	assertRepositoryPermissionSerialization(t, ctx, pool, owner, create)
	// Revocation affects discovery and exact retries, not just new mutations.
	if _, e := pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE workspace_id=$1 AND user_id=$2`, owner.WorkspaceID, member.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ReadConfiguration(ctx, member, "repository", repo.ID); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("suspended member read", e)
	}
	if _, e := s.ListConfiguration(ctx, member, goldenpath.ConfigurationQuery{Kind: "repository-components", ScopeID: repo.ID, Limit: 25}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("suspended member discovery", e)
	}
	if _, e := pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET status='active' WHERE workspace_id=$1 AND user_id=$2`, owner.WorkspaceID, member.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `UPDATE radishnexus.user_accounts SET status='disabled' WHERE user_id=$1`, owner.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := call(owner, create); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("disabled owner replayed create", e)
	}
	if _, e := call(owner, unlink); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("disabled owner replayed unlink", e)
	}
	if _, e := pool.Exec(ctx, `UPDATE radishnexus.user_accounts SET status='active' WHERE user_id=$1`, owner.ID); e != nil {
		t.Fatal(e)
	}
}

// Permission changes are controlled fault injection: no account/membership
// administration endpoint is introduced by this feature.
func assertRepositoryPermissionSerialization(t *testing.T, ctx context.Context, pool *pgxpool.Pool, owner authz.Principal, base goldenpath.ConfigurationInput) {
	t.Helper()
	service := goldenpath.NewConfigurationService(goldenpostgres.New(pool), goldenpath.CryptoIDGenerator{}, goldenpath.SystemClock{})
	for _, table := range []string{"user_accounts", "workspace_memberships"} {
		for _, writeFirst := range []bool{false, true} {
			label := fmt.Sprintf("%s-%v", table, writeFirst)
			in := base
			in.ClientOperationID = "permission-race-" + label
			metadata := *base.Repository
			metadata.ExternalID = in.ClientOperationID
			in.Repository = &metadata
			call := func() error {
				_, e := service.Configure(ctx, goldenpath.Invocation{Principal: owner, SourceKind: "web", CorrelationID: label}, in)
				return e
			}
			status := "disabled"
			if table == "workspace_memberships" {
				status = "suspended"
			}
			update := fmt.Sprintf("UPDATE radishnexus.%s SET status=$1 WHERE user_id=$2", table)
			wait := func(pattern string) {
				t.Helper()
				timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				for {
					var waiting bool
					if e := pool.QueryRow(timeout, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE $1)`, pattern).Scan(&waiting); e != nil {
						t.Fatal(e)
					}
					if waiting {
						return
					}
					select {
					case <-timeout.Done():
						t.Fatal("permission race barrier", label)
					case <-time.After(5 * time.Millisecond):
					}
				}
			}
			if !writeFirst {
				tx, e := pool.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = tx.Exec(ctx, update, status, owner.ID); e != nil {
					tx.Rollback(ctx)
					t.Fatal(e)
				}
				done := make(chan error, 1)
				go func() { done <- call() }()
				wait("%FROM radishnexus." + table + "%")
				if e = tx.Commit(ctx); e != nil {
					t.Fatal(e)
				}
				if e = <-done; !errors.Is(e, authz.ErrNotFound) {
					t.Fatal("write crossed committed revocation", label, e)
				}
			} else {
				if _, e := pool.Exec(ctx, `CREATE FUNCTION radishnexus.test_repository_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(33,1); RETURN NEW; END $$; CREATE TRIGGER test_repository_barrier BEFORE INSERT ON radishnexus.repositories FOR EACH ROW EXECUTE FUNCTION radishnexus.test_repository_barrier()`); e != nil {
					t.Fatal(e)
				}
				gate, e := pool.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = gate.Exec(ctx, `SELECT pg_advisory_xact_lock(33,1)`); e != nil {
					gate.Rollback(ctx)
					t.Fatal(e)
				}
				done, revoked := make(chan error, 1), make(chan error, 1)
				go func() { done <- call() }()
				wait("%INSERT INTO radishnexus.repositories%")
				go func() { _, e := pool.Exec(ctx, update, status, owner.ID); revoked <- e }()
				wait("%UPDATE radishnexus." + table + "%")
				if e = gate.Commit(ctx); e != nil {
					t.Fatal(e)
				}
				if e = <-done; e != nil {
					t.Fatal("authorized write failed", label, e)
				}
				if e = <-revoked; e != nil {
					t.Fatal("revocation failed", label, e)
				}
				if _, e = pool.Exec(ctx, `DROP TRIGGER test_repository_barrier ON radishnexus.repositories; DROP FUNCTION radishnexus.test_repository_barrier()`); e != nil {
					t.Fatal(e)
				}
			}
			if e := call(); !errors.Is(e, authz.ErrNotFound) {
				t.Fatal("receipt bypassed permission", label, e)
			}
			var count int
			if e := pool.QueryRow(ctx, `SELECT count(*) FROM radishnexus.repositories WHERE external_id=$1`, metadata.ExternalID).Scan(&count); e != nil {
				t.Fatal(e)
			}
			want := 0
			if writeFirst {
				want = 1
			}
			if count != want {
				t.Fatal("unexpected serialized facts", label, count)
			}
			if _, e := pool.Exec(ctx, update, "active", owner.ID); e != nil {
				t.Fatal(e)
			}
		}
	}
}
