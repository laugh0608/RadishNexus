package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
)

// ValidateJenkinsSource is read-only. Persisted capability facts are claimed
// only by the first successful v2 delivery, never by startup or readiness.
func (s *Store) ValidateJenkinsSource(ctx context.Context, workspace, component, source, job string, binding *goldenpath.CICheckoutBinding) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Jenkins source validation: %w", err)
	}
	defer rollback(ctx, tx, &err)
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM radishnexus.components WHERE workspace_id=$1 AND id=$2)`, workspace, component).Scan(&exists); err != nil {
		return fmt.Errorf("validate Jenkins Component: %w", err)
	}
	if !exists {
		return authz.ErrNotFound
	}
	if err = checkCheckoutBinding(ctx, tx, workspace, component, source, job, binding, false, time.Time{}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func checkCheckoutBinding(ctx context.Context, tx pgx.Tx, workspace, component, source, job string, binding *goldenpath.CICheckoutBinding, claim bool, at time.Time) error {
	var existing goldenpath.CICheckoutBinding
	var storedComponent, storedJob string
	err := tx.QueryRow(ctx, `SELECT component_id,job_full_name,binding_id,repository_id,provider,provider_origin,external_id,profile,effective_from_build
        FROM radishnexus.ci_source_checkout_bindings WHERE workspace_id=$1 AND source_kind='jenkins' AND source_id=$2`, workspace, source).Scan(
		&storedComponent, &storedJob, &existing.BindingID, &existing.RepositoryID, &existing.Provider, &existing.ProviderOrigin, &existing.ExternalID, &existing.Profile, &existing.EffectiveFromBuild)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read CI checkout binding: %w", err)
	}
	exists := err == nil
	if exists && (binding == nil || existing != *binding || component != storedComponent || job != storedJob) {
		return fmt.Errorf("%w: persisted CI checkout binding mismatch", authz.ErrConflict)
	}
	if binding == nil {
		return nil
	}
	normalized, valid := goldenpath.NormalizeCICheckoutBinding(*binding)
	if !valid || normalized != *binding {
		return authz.ErrInvalid
	}
	var provider, origin, external string
	err = tx.QueryRow(ctx, `SELECT provider,provider_origin,external_id FROM radishnexus.repositories WHERE workspace_id=$1 AND id=$2 FOR KEY SHARE`, workspace, binding.RepositoryID).Scan(&provider, &origin, &external)
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read checkout Repository identity: %w", err)
	}
	if provider != binding.Provider || origin != binding.ProviderOrigin || external != binding.ExternalID {
		return fmt.Errorf("%w: checkout Repository identity mismatch", authz.ErrConflict)
	}
	if exists {
		return nil
	}
	// Reject an activation boundary that would reinterpret existing v1 facts.
	// Legacy internal adapters may have nonnumeric keys; never guess a number.
	var incompatible bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM radishnexus.ci_runs WHERE workspace_id=$1 AND source_kind='jenkins' AND source_id=$2
        AND (component_id<>$3 OR CASE WHEN external_run_key ~ '^[1-9][0-9]{0,9}$' THEN external_run_key::bigint >= $4 ELSE true END))`, workspace, source, component, binding.EffectiveFromBuild).Scan(&incompatible)
	if err != nil {
		return fmt.Errorf("check checkout activation history: %w", err)
	}
	if incompatible {
		return fmt.Errorf("%w: checkout activation overlaps legacy runs", authz.ErrConflict)
	}
	if !claim {
		return nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO radishnexus.ci_source_checkout_bindings(workspace_id,source_kind,source_id,component_id,job_full_name,binding_id,repository_id,provider,provider_origin,external_id,profile,effective_from_build,recorded_at)
        VALUES($1,'jenkins',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, workspace, source, component, job, binding.BindingID, binding.RepositoryID, binding.Provider, binding.ProviderOrigin, binding.ExternalID, binding.Profile, binding.EffectiveFromBuild, at)
	if err != nil {
		return mapDatabaseError("claim CI checkout binding", err)
	}
	return nil
}

type checkoutLinkMetadata struct {
	SchemaVersion int    `json:"schema_version"`
	Profile       string `json:"profile"`
	BindingID     string `json:"binding_id"`
	Algorithm     string `json:"algorithm"`
	OID           string `json:"oid"`
}

func insertCheckoutLink(ctx context.Context, tx pgx.Tx, c goldenpath.RecordCompletedCIRunCommand) error {
	if c.Checkout == nil || c.Checkout.State != "observed" {
		return nil
	}
	m := checkoutLinkMetadata{1, c.CheckoutBinding.Profile, c.CheckoutBinding.BindingID, c.Checkout.Revision.Algorithm, c.Checkout.Revision.OID}
	metadata, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode CI checkout metadata: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO radishnexus.entity_links(id,workspace_id,from_type,from_id,relation_type,to_type,to_id,assertion,origin,origin_ref,created_by_kind,created_by_id,created_at,updated_at,source_event_id,metadata)
        VALUES($1,$2,'ci-run',$3,'built-from','repository',$4,'asserted','plugin',$5,'plugin',$6,$7,$7,$8,$9)`, c.CheckoutLinkID, c.WorkspaceID, c.CIRunID, c.CheckoutBinding.RepositoryID, c.DeliveryID, c.SourceID, c.RecordedAt, c.EventID, metadata)
	if err != nil {
		return mapDatabaseError("insert CI checkout relationship", err)
	}
	return nil
}

func loadCheckoutProjection(ctx context.Context, tx pgx.Tx, p authz.Principal, ciRun string) (*goldenpath.CICheckoutProjection, error) {
	c := &goldenpath.CICheckoutProjection{Visibility: goldenpath.ProjectionVisible}
	var repository *string
	err := tx.QueryRow(ctx, `SELECT c.checkout_state,COALESCE(c.checkout_reason,''),b.repository_id FROM radishnexus.ci_runs c
        LEFT JOIN radishnexus.ci_source_checkout_bindings b ON b.workspace_id=c.workspace_id AND b.source_kind=c.source_kind AND b.source_id=c.source_id AND b.binding_id=c.checkout_binding_id
        WHERE c.workspace_id=$1 AND c.id=$2`, p.WorkspaceID, ciRun).Scan(&c.State, &c.Reason, &repository)
	if err != nil {
		return nil, fmt.Errorf("read CI checkout: %w", err)
	}
	if c.State == "not_collected" {
		if repository != nil || c.Reason != "" {
			return nil, errors.New("inconsistent legacy checkout")
		}
		return c, nil
	}
	if repository == nil {
		return nil, errors.New("missing CI checkout binding")
	}
	ref := entityref.Ref{Type: "repository", ID: *repository}
	exists, readable, err := entityAccess(ctx, tx, p, ref)
	if err != nil {
		return nil, err
	}
	if !exists || !readable {
		return &goldenpath.CICheckoutProjection{Visibility: goldenpath.ProjectionRestricted}, nil
	}
	c.Repository, err = visibleSubjectProjection(ctx, tx, p, ref)
	if err != nil {
		return nil, err
	}
	if c.State == "observed" {
		c.Revision = &goldenpath.GitRevision{}
		err = tx.QueryRow(ctx, `SELECT metadata->>'algorithm',metadata->>'oid' FROM radishnexus.entity_links
            WHERE workspace_id=$1 AND from_type='ci-run' AND from_id=$2 AND relation_type='built-from' AND to_type='repository' AND to_id=$3 AND state='active'`, p.WorkspaceID, ciRun, *repository).Scan(&c.Revision.Algorithm, &c.Revision.OID)
		if err != nil {
			return nil, fmt.Errorf("read CI checkout revision: %w", err)
		}
	}
	return c, nil
}

func (s *Store) ListRepositoryCIRuns(ctx context.Context, p authz.Principal, repository string, in goldenpath.DiscoveryPageInput) (page goldenpath.RepositoryCIRunPage, err error) {
	if err := p.ValidateUser(); err != nil {
		return page, err
	}
	tx, err := s.beginAdmission(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return page, fmt.Errorf("begin Repository CI Runs: %w", err)
	}
	defer rollback(ctx, tx, &err)
	exists, readable, err := entityAccess(ctx, tx, p, entityref.Ref{Type: "repository", ID: repository})
	if err != nil {
		return page, err
	}
	if !exists || !readable {
		return page, authz.ErrNotFound
	}
	page.Items = []goldenpath.RepositoryCIRunItem{}
	after := in.AfterID
	for len(page.Items) <= in.Limit {
		rows, e := tx.Query(ctx, `SELECT c.id,c.status,c.completed_at,l.metadata->>'algorithm',l.metadata->>'oid'
            FROM radishnexus.entity_links l JOIN radishnexus.ci_runs c ON c.workspace_id=l.workspace_id AND c.id=l.from_id
            WHERE l.workspace_id=$1 AND l.to_id=$2 AND l.from_id>$3 AND l.from_type='ci-run' AND l.to_type='repository'
            AND l.relation_type='built-from' AND l.state='active' ORDER BY l.from_id LIMIT $4`, p.WorkspaceID, repository, after, in.Limit+1)
		if e != nil {
			return page, fmt.Errorf("list Repository CI Run facts: %w", e)
		}
		facts := []goldenpath.RepositoryCIRunItem{}
		for rows.Next() {
			f := goldenpath.RepositoryCIRunItem{Ref: entityref.Ref{Type: "ci-run"}}
			if e = rows.Scan(&f.Ref.ID, &f.Status, &f.CompletedAt, &f.Revision.Algorithm, &f.Revision.OID); e != nil {
				rows.Close()
				return page, fmt.Errorf("scan Repository CI Run: %w", e)
			}
			facts = append(facts, f)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return page, fmt.Errorf("iterate Repository CI Runs: %w", e)
		}
		for _, f := range facts {
			after = f.Ref.ID
			exists, readable, e := entityAccess(ctx, tx, p, f.Ref)
			if e != nil {
				return page, e
			}
			if !exists || !readable {
				continue
			}
			page.Items = append(page.Items, f)
			if len(page.Items) > in.Limit {
				page.Items = page.Items[:in.Limit]
				page.NextID = page.Items[in.Limit-1].Ref.ID
				break
			}
		}
		if page.NextID != "" || len(facts) < in.Limit+1 {
			break
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return page, fmt.Errorf("commit Repository CI Runs: %w", err)
	}
	return page, nil
}
