//go:build integration

package postgres_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	goldenpostgres "github.com/laugh0608/RadishNexus/server/internal/goldenpath/postgres"
	"github.com/laugh0608/RadishNexus/server/internal/jenkins"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
)

func TestCICheckoutTransactionsHistoryAndDiscovery(t *testing.T) {
	ctx, pool, s, _, cfg, component := ticketComponentFixture(t)
	cmp := component("checkout")
	repo, e := cfg.Configure(ctx, tcInvocation("usr_admin"), goldenpath.ConfigurationInput{Kind: "repository.create", ScopeID: "wrk_main", ClientOperationID: "checkout-repository", Name: "Checkout source", Repository: &goldenpath.RepositoryConfigurationInput{RepositoryMetadata: goldenpath.RepositoryMetadata{Provider: "gitea", ProviderOrigin: "https://git.example.test", ExternalID: "123", WebURL: "https://git.example.test/team/source", DefaultBranch: "main"}}})
	if e != nil {
		t.Fatal(e)
	}
	binding := goldenpath.CICheckoutBinding{BindingID: "checkout_a", RepositoryID: repo.Object.ID, Provider: "gitea", ProviderOrigin: "https://git.example.test", ExternalID: "123", Profile: goldenpath.JenkinsCheckoutProfile, EffectiveFromBuild: 42}
	source := jenkins.Source{ID: "checkout_source", WorkspaceID: "wrk_main", ComponentID: cmp, Job: "example/build", CheckoutBinding: &binding}
	store := goldenpostgres.New(pool)
	if e := store.ValidateJenkinsSource(ctx, "wrk_main", cmp, source.ID, source.Job, &binding); e != nil {
		t.Fatal(e)
	}
	assertTableCount(t, ctx, pool, "radishnexus.ci_source_checkout_bindings", 0)
	payload := func(n int64, state string) jenkins.Payload {
		p := jenkins.Payload{Version: 1, Job: source.Job, Number: n, Result: "SUCCESS", CompletedAt: time.Now().UTC().Truncate(time.Millisecond)}
		if state != "" {
			p.Version = 2
			p.Checkout = &goldenpath.CICheckout{BindingID: binding.BindingID, State: state}
			if state == "observed" {
				p.Checkout.Revision = &goldenpath.GitRevision{Algorithm: "sha1", OID: strings.Repeat("a", 40)}
			} else {
				reason := "checkout_not_observed"
				p.Checkout.Reason = &reason
			}
		}
		return p
	}
	record := func(p jenkins.Payload) goldenpath.CIRunReceipt {
		t.Helper()
		d, in, e := source.Map(p, p.DeliveryID())
		if e != nil {
			t.Fatal(e)
		}
		r, e := s.RecordCompletedJenkinsRun(ctx, d, in)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	old := payload(41, "")
	legacy := record(old)
	assertTableCount(t, ctx, pool, "radishnexus.ci_source_checkout_bindings", 0)
	p := payload(42, "observed")
	signed := recordCheckoutHTTP(t, ctx, s, source, p)
	first := record(p)
	if first.CIRun.ID != signed.CIRunID {
		t.Fatal("signed receipt drift")
	}
	if first.CIRun.CheckoutState != "observed" || !record(p).Duplicate || !record(old).Duplicate {
		t.Fatal("mixed-version retry")
	}
	assertTableCount(t, ctx, pool, "radishnexus.ci_source_checkout_bindings", 1)
	missing := record(payload(43, "unavailable"))
	sameCommit := record(payload(44, "observed"))
	if sameCommit.CIRun.ID == first.CIRun.ID {
		t.Fatal("different builds collapsed")
	}
	reader := principal("usr_reader")
	for id, state := range map[string]string{legacy.CIRun.ID: "not_collected", first.CIRun.ID: "observed", missing.CIRun.ID: "unavailable"} {
		v, e := s.GetNexusView(ctx, reader, entityref.Ref{Type: "ci-run", ID: id})
		if e != nil || v.Current.Checkout.State != state {
			t.Fatal("checkout current", e, v.Current)
		}
		if state == "observed" && (len(v.Relations) != 1 || v.Current.Checkout.Revision.OID != strings.Repeat("a", 40) || len(v.Timeline[0].Subjects) != 2) {
			t.Fatal("checkout evidence", v)
		}
	}
	page, e := s.ListRepositoryCIRuns(ctx, reader, repo.Object.ID, goldenpath.DiscoveryPageInput{Limit: 1})
	if e != nil || len(page.Items) != 1 || page.NextID == "" {
		t.Fatal("first reverse page", page, e)
	}
	next, e := s.ListRepositoryCIRuns(ctx, reader, repo.Object.ID, goldenpath.DiscoveryPageInput{Limit: 1, AfterID: page.NextID})
	if e != nil || len(next.Items) != 1 || next.NextID != "" || next.Items[0].Ref.ID == page.Items[0].Ref.ID {
		t.Fatal("second reverse page", next, e)
	}
	if _, e := store.RebuildActivityProjection(ctx); e != nil {
		t.Fatal(e)
	}
	changed := p
	c := *p.Checkout
	r := *c.Revision
	r.OID = strings.Repeat("b", 40)
	c.Revision = &r
	changed.Checkout = &c
	d, in, _ := source.Map(changed, changed.DeliveryID())
	if _, e = s.RecordCompletedJenkinsRun(ctx, d, in); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("revision overwrite", e)
	}
	for _, b := range []*goldenpath.CICheckoutBinding{nil, func() *goldenpath.CICheckoutBinding { b := binding; b.EffectiveFromBuild++; return &b }()} {
		if e = store.ValidateJenkinsSource(ctx, "wrk_main", cmp, source.ID, source.Job, b); !errors.Is(e, authz.ErrConflict) {
			t.Fatal("binding replaced", e)
		}
	}
	for _, query := range []string{
		`UPDATE radishnexus.ci_runs SET checkout_state='not_collected',checkout_binding_id=NULL WHERE id=$1`,
		`UPDATE radishnexus.entity_links SET metadata=jsonb_set(metadata,'{oid}','"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"') WHERE from_id=$1`,
		`UPDATE radishnexus.entity_links SET state='removed' WHERE from_id=$1`,
		`DELETE FROM radishnexus.entity_links WHERE from_id=$1`,
	} {
		_, e := pool.Exec(ctx, query, first.CIRun.ID)
		assertPGCode(t, e, "23514", "immutable checkout")
	}
	// Failure after claiming a fresh binding rolls the complete transaction back.
	broken := source
	broken.ID = "checkout_failure"
	failure := payload(50, "observed")
	d, in, _ = broken.Map(failure, failure.DeliveryID())
	var event string
	if e = pool.QueryRow(ctx, `SELECT event_id FROM radishnexus.inbound_deliveries WHERE ci_run_id=$1`, first.CIRun.ID).Scan(&event); e != nil {
		t.Fatal(e)
	}
	failService := goldenpath.NewService(store, &fixedIDs{values: []string{"cir_fail", event, "cor_fail", "lnk_fail"}}, fixedClock{now: time.Now()})
	if _, e = failService.RecordCompletedJenkinsRun(ctx, d, in); e == nil {
		t.Fatal("expected rollback")
	}
	assertAbsent(t, ctx, pool, "radishnexus.ci_runs", "cir_fail")
	assertTableCount(t, ctx, pool, "radishnexus.ci_source_checkout_bindings", 1)
	// Inject each transaction write failure, including late projection/outbox writes.
	tables := []string{"ci_source_checkout_bindings", "inbound_deliveries", "ci_runs", "domain_events", "entity_links", "activity_items", "outbox_deliveries"}
	counts := func() []int {
		t.Helper()
		out := []int{}
		for _, table := range tables {
			var n int
			if e := pool.QueryRow(ctx, "SELECT count(*) FROM radishnexus."+table).Scan(&n); e != nil {
				t.Fatal(e)
			}
			out = append(out, n)
		}
		return out
	}
	for i, table := range tables {
		before := counts()
		if _, e := pool.Exec(ctx, `CREATE FUNCTION radishnexus.fail_checkout() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected checkout failure'; END $$; CREATE TRIGGER fail_checkout BEFORE INSERT ON radishnexus.`+table+` FOR EACH ROW EXECUTE FUNCTION radishnexus.fail_checkout()`); e != nil {
			t.Fatal(e)
		}
		failureSource := source
		failureSource.ID = fmt.Sprintf("checkout_fail_%d", i)
		d, in, e := failureSource.Map(payload(60, "observed"), "build-60")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.RecordCompletedJenkinsRun(ctx, d, in); e == nil || !strings.Contains(e.Error(), "injected checkout failure") {
			t.Fatal("failure hidden", table, e)
		}
		if !reflect.DeepEqual(before, counts()) {
			t.Fatal("partial checkout transaction", table)
		}
		if _, e = pool.Exec(ctx, `DROP TRIGGER fail_checkout ON radishnexus.`+table+`; DROP FUNCTION radishnexus.fail_checkout()`); e != nil {
			t.Fatal(e)
		}
	}
	// Invalid target identity cannot establish a source capability.
	cross := binding
	cross.RepositoryID = "rep_missing"
	if e = store.ValidateJenkinsSource(ctx, "wrk_other", cmp, source.ID, source.Job, &binding); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("cross Workspace binding", e)
	}
	if e = store.ValidateJenkinsSource(ctx, "wrk_main", cmp, "new_source", source.Job, &cross); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal("missing Repository", e)
	}
	cross = binding
	cross.ExternalID = "different"
	if e = store.ValidateJenkinsSource(ctx, "wrk_main", cmp, "new_source", source.Job, &cross); !errors.Is(e, authz.ErrConflict) {
		t.Fatal("provider identity mismatch", e)
	}
	// Concurrent delivery serialization yields one authoritative fact.
	concurrent := payload(45, "observed")
	d, in, _ = source.Map(concurrent, concurrent.DeliveryID())
	type result struct {
		r goldenpath.CIRunReceipt
		e error
	}
	ch := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() { r, e := s.RecordCompletedJenkinsRun(ctx, d, in); ch <- result{r, e} }()
	}
	a, b := <-ch, <-ch
	if a.e != nil || b.e != nil || a.r.Duplicate == b.r.Duplicate || a.r.CIRun.ID != b.r.CIRun.ID {
		t.Fatal("concurrent", a, b)
	}

	// Removing a manual association does not revoke the separately configured capability.
	manual, e := cfg.Configure(ctx, tcInvocation("usr_admin"), goldenpath.ConfigurationInput{Kind: "component.repository.link", ScopeID: cmp, ClientOperationID: "checkout-manual-link", Repository: &goldenpath.RepositoryConfigurationInput{RepositoryID: repo.Object.ID, Confirmed: true}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = cfg.Configure(ctx, tcInvocation("usr_admin"), goldenpath.ConfigurationInput{Kind: "component.repository.unlink", ScopeID: cmp, ClientOperationID: "checkout-manual-unlink", Repository: &goldenpath.RepositoryConfigurationInput{LinkID: manual.LinkID, Confirmed: true}}); e != nil {
		t.Fatal(e)
	}
	if !record(p).Duplicate {
		t.Fatal("manual unlink changed machine history")
	}
	// Two competing first bindings cannot both claim one source.
	raceSource := source
	raceSource.ID = "checkout_binding_race"
	racePayload := payload(70, "observed")
	otherSource := raceSource
	otherBinding := binding
	otherBinding.BindingID = "checkout_competing"
	otherSource.CheckoutBinding = &otherBinding
	otherPayload := racePayload
	otherCheckout := *racePayload.Checkout
	otherCheckout.BindingID = otherBinding.BindingID
	otherPayload.Checkout = &otherCheckout
	outcomes := make(chan error, 2)
	for i, src := range []jenkins.Source{raceSource, otherSource} {
		p := racePayload
		if i == 1 {
			p = otherPayload
		}
		go func(src jenkins.Source, p jenkins.Payload) {
			d, in, e := src.Map(p, p.DeliveryID())
			if e == nil {
				_, e = s.RecordCompletedJenkinsRun(ctx, d, in)
			}
			outcomes <- e
		}(src, p)
	}
	ea, eb := <-outcomes, <-outcomes
	if !((ea == nil && errors.Is(eb, authz.ErrConflict)) || (eb == nil && errors.Is(ea, authz.ErrConflict))) {
		t.Fatal("competing bindings", ea, eb)
	}
	// Current account suspension denies both source and reverse discovery.
	if _, e = pool.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET status='suspended' WHERE workspace_id='wrk_main' AND user_id='usr_reader'`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.GetNexusView(ctx, reader, entityref.Ref{Type: "ci-run", ID: first.CIRun.ID}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal(e)
	}
	if _, e = s.ListRepositoryCIRuns(ctx, reader, repo.Object.ID, goldenpath.DiscoveryPageInput{Limit: 25}); !errors.Is(e, authz.ErrNotFound) {
		t.Fatal(e)
	}
	assertNoDeploymentEvent(t, ctx, pool)
	// A legacy build at/above N prevents activating a source retroactively.
	overlap := source
	overlap.ID = "checkout_overlap"
	overlap.CheckoutBinding = nil
	prior := payload(51, "")
	d, in, _ = overlap.Map(prior, prior.DeliveryID())
	if _, e = s.RecordCompletedJenkinsRun(ctx, d, in); e != nil {
		t.Fatal(e)
	}
	if e = store.ValidateJenkinsSource(ctx, "wrk_main", cmp, overlap.ID, overlap.Job, &binding); !errors.Is(e, authz.ErrConflict) {
		t.Fatal(fmt.Sprint("legacy overlap: ", e))
	}
}

// Signed synthetic input exercises the production receiver; it is not a real SCM observation.
func recordCheckoutHTTP(t *testing.T, ctx context.Context, svc *goldenpath.Service, source jenkins.Source, payload jenkins.Payload) jenkins.SendResult {
	t.Helper()
	binding, _ := json.Marshal(source.CheckoutBinding)
	config := fmt.Sprintf(`{"version":2,"sources":[{"source_id":%q,"workspace_id":%q,"component_id":%q,"job_full_name":%q,"checkout_binding":%s,"keys":[{"key_id":"synthetic","secret_file":"/synthetic/key"}]}]}`, source.ID, source.WorkspaceID, source.ComponentID, source.Job, binding)
	key := strings.Repeat("ab", 32)
	sources, e := jenkins.LoadSources([]byte(config), func(string, int64) ([]byte, error) { return []byte(key), nil })
	if e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(payload)
	// Payload timestamps are encoded by time.Time in the same accepted UTC form.
	policy, _ := httptransport.NewBrowserSessionPolicy("https://nexus.example.test")
	proxy, _ := httptransport.NewTrustedProxyPolicy("127.0.0.1/32")
	h := httptransport.WithRequestID(httptransport.NewJenkinsDeliveryHandler(sources, svc, policy, proxy, time.Now, func(httptransport.JenkinsDeliveryLog) {}))
	req := httptest.NewRequest("POST", "https://nexus.example.test"+jenkins.Path(source.ID), strings.NewReader(string(body))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	sum := sha256.Sum256(body)
	secret, _ := hex.DecodeString(key)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strings.Join([]string{"radishnexus-jenkins-v1", "POST", jenkins.Path(source.ID), "synthetic", payload.DeliveryID(), stamp, hex.EncodeToString(sum[:])}, "\n")))
	req.Header.Set("X-Nexus-Key-ID", "synthetic")
	req.Header.Set("X-Nexus-Delivery-ID", payload.DeliveryID())
	req.Header.Set("X-Nexus-Timestamp", stamp)
	req.Header.Set("X-Nexus-Signature", "v1="+hex.EncodeToString(mac.Sum(nil)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 201 && w.Code != 200 {
		t.Fatal("signed checkout delivery", w.Code, w.Body.String())
	}
	var result struct {
		Data struct {
			CIRunID   string `json:"ci_run_id"`
			Duplicate bool   `json:"duplicate"`
		} `json:"data"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil || result.Data.CIRunID == "" {
		t.Fatal("signed receipt", e, w.Body.String())
	}
	return jenkins.SendResult{CIRunID: result.Data.CIRunID, Duplicate: result.Data.Duplicate}
}
