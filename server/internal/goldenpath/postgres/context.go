package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"strings"
	"time"
)

func (s *Store) contextTransaction(ctx context.Context, iso pgx.TxIsoLevel, fn func(pgx.Tx) (bool, error)) (err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: iso})
	if err != nil {
		return ownerDBError("begin context transaction", err)
	}
	defer func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if e := tx.Rollback(c); e != nil && !errors.Is(e, pgx.ErrTxClosed) {
			err = errors.Join(err, ownerDBError("rollback context transaction", e))
		}
	}()
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='5s'`); err != nil {
		return ownerDBError("context lock budget", err)
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(739140967704144)`); err != nil {
		return ownerDBError("lock context schema", err)
	}
	changed, err := fn(tx)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		var pgerr *pgconn.PgError
		if changed && !errors.Is(err, pgx.ErrTxCommitRollback) && !(errors.As(err, &pgerr) && pgerr.Severity == "ERROR") {
			return errors.Join(cp.ErrUnconfirmed, ownerDBError("context commit acknowledgement", err))
		}
		return ownerDBError("commit context transaction", err)
	}
	return nil
}
func contextOwner(ctx context.Context, tx pgx.Tx, a goldenpath.OwnerAccess) (ownerSessionState, error) {
	session, m, e := lockOwnerReader(ctx, tx, a)
	if e != nil {
		return session, e
	}
	if m.role != "owner" {
		return session, authz.ErrForbidden
	}
	_, e = ownerSessionNow(ctx, tx, session, true)
	return session, e
}

// ContextStore receives the application composition root's exact embedded-schema
// checker. The persistence package does not depend on migration ownership.
type ContextStore struct {
	*Store
	schemaCheck func(context.Context, pgx.Tx) error
}

func NewContextStore(pool *pgxpool.Pool, check func(context.Context, pgx.Tx) error) *ContextStore {
	return &ContextStore{Store: New(pool), schemaCheck: check}
}
func (s *ContextStore) contextSchema(ctx context.Context, tx pgx.Tx) error {
	if s.schemaCheck == nil {
		return errors.New("context schema checker is required")
	}
	if e := s.schemaCheck(ctx, tx); e != nil {
		return ownerDBError("context schema", e)
	}
	return nil
}

func contextIdentity(ctx context.Context, tx pgx.Tx, workspace string) (string, error) {
	var value string
	e := tx.QueryRow(ctx, `SELECT c.system_identifier::text||'/'||d.oid::text||'/'||d.datname||'/'||w.id||'/'||extract(epoch from w.created_at)::text FROM pg_control_system() c CROSS JOIN pg_database d CROSS JOIN radishnexus.workspaces w WHERE d.datname=current_database() AND w.id=$1`, workspace).Scan(&value)
	if e != nil {
		return "", ownerDBError("context target identity", e)
	}
	return cp.Hash([]byte(value)), nil
}

// Business tables are locked in a fixed order before the emptiness check. This
// blocks ordinary writers too; the import lock alone would not do that.
var contextBusinessTables = []string{"activity_items", "channels", "ci_runs", "ci_source_checkout_bindings", "collaboration_command_receipts", "components", "context_export_audit", "context_import_batches", "context_import_history", "context_import_mappings", "decisions", "deployments", "document_revisions", "documents", "domain_events", "entity_links", "environment_deployment_authorizations", "environments", "inbound_deliveries", "messages", "outbox_deliveries", "projects", "repositories", "teams", "threads", "tickets"}

func emptyContextTarget(ctx context.Context, tx pgx.Tx, lock bool) error {
	if lock {
		names := make([]string, len(contextBusinessTables))
		for i, t := range contextBusinessTables {
			names[i] = "radishnexus." + t
		}
		if _, e := tx.Exec(ctx, "LOCK TABLE "+strings.Join(names, ",")+" IN SHARE ROW EXCLUSIVE MODE"); e != nil {
			return ownerDBError("lock empty context target", e)
		}
	}
	for _, t := range contextBusinessTables {
		var any bool
		if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM radishnexus."+t+")").Scan(&any); e != nil {
			return ownerDBError("check empty context target", e)
		}
		if any {
			return authz.ErrConflict
		}
	}
	return nil
}
func (s *ContextStore) PrepareContext(ctx context.Context, a goldenpath.OwnerAccess, b cp.Bundle, operation string) (out cp.Plan, err error) {
	d, _, err := cp.Unpack(b)
	if err != nil {
		return out, err
	}
	err = s.contextTransaction(ctx, pgx.RepeatableRead, func(tx pgx.Tx) (bool, error) {
		session, e := contextOwner(ctx, tx, a)
		if e != nil {
			return false, e
		}
		if e = s.contextSchema(ctx, tx); e != nil {
			return false, e
		}
		if e = emptyContextTarget(ctx, tx, false); e != nil {
			return false, e
		}
		identity, e := contextIdentity(ctx, tx, a.Principal.WorkspaceID)
		if e != nil {
			return false, e
		}
		out, e = cp.BuildPlan(b.Manifest, d, a.Principal.WorkspaceID, identity, a.Principal.ID, operation)
		if e != nil {
			return false, e
		}
		_, e = ownerSessionNow(ctx, tx, session, true)
		return false, e
	})
	return
}
func contextResult(ctx context.Context, tx pgx.Tx, a goldenpath.OwnerAccess, op string) (out cp.Result, err error) {
	out = cp.Result{Status: "not_observed", OperationID: op}
	var when time.Time
	err = tx.QueryRow(ctx, `SELECT package_digest,plan_digest,project_id,applied_at FROM radishnexus.context_import_batches WHERE operation_id=$1 AND workspace_id=$2 AND actor_id=$3`, op, a.Principal.WorkspaceID, a.Principal.ID).Scan(&out.PackageDigest, &out.PlanDigest, &out.ProjectID, &when)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, ownerDBError("read context receipt", err)
	}
	out.Status = "applied"
	out.AppliedAt = &when
	return out, nil
}
func (s *ContextStore) ContextResult(ctx context.Context, a goldenpath.OwnerAccess, op string) (out cp.Result, err error) {
	err = s.contextTransaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) (bool, error) {
		session, _, e := lockOwnerReader(ctx, tx, a)
		if e != nil {
			return false, e
		}
		out, e = contextResult(ctx, tx, a, op)
		if e != nil {
			return false, e
		}
		_, e = ownerSessionNow(ctx, tx, session, false)
		return false, e
	})
	return
}
func (s *ContextStore) ReadContextSource(ctx context.Context, p authz.Principal, ref entityref.Ref) (out goldenpath.ContextSource, err error) {
	err = s.contextTransaction(ctx, pgx.RepeatableRead, func(tx pgx.Tx) (bool, error) {
		exists, readable, e := entityAccess(ctx, tx, p, ref)
		if e != nil {
			return false, e
		}
		if !exists || !readable {
			return false, authz.ErrNotFound
		}
		var when time.Time
		e = tx.QueryRow(ctx, `SELECT b.applied_at FROM radishnexus.context_import_mappings m JOIN radishnexus.context_import_batches b USING(workspace_id,operation_id) WHERE m.workspace_id=$1 AND m.kind=$2 AND m.target_id=$3`, p.WorkspaceID, ref.Type, ref.ID).Scan(&when)
		if errors.Is(e, pgx.ErrNoRows) {
			return false, nil
		}
		if e != nil {
			return false, ownerDBError("read context source", e)
		}
		out.Imported = true
		out.AppliedAt = when.UTC().Format(time.RFC3339)
		return false, nil
	})
	return
}
func contextJSONRows[T any](ctx context.Context, tx pgx.Tx, query string, limit int, args ...any) ([]T, error) {
	rows, e := tx.Query(ctx, query, args...)
	if e != nil {
		return nil, ownerDBError("read context records", e)
	}
	defer rows.Close()
	out := []T{}
	size := 0
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			return nil, ownerDBError("scan context records", e)
		}
		size += len(raw)
		if len(out) >= limit || size > cp.MaxBytes {
			return nil, cp.ErrIncomplete
		}
		var v T
		if e = json.Unmarshal(raw, &v); e != nil {
			return nil, cp.ErrIncomplete
		}
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return nil, ownerDBError("read context records", e)
	}
	return out, nil
}
