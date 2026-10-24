// Package workspaceauth coordinates Workspace admission and membership changes.
// It does not replace object authorization or hold locks across network I/O.
package workspaceauth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

// Lock must precede account, session, membership and object row locks. The
// schema lock precedes the Workspace lock, matching the explicit runner.
func Lock(ctx context.Context, tx pgx.Tx, workspace string, exclusive bool) error {
	if workspace == "" {
		return authz.ErrInvalid
	}
	if err := SchemaLock(ctx, tx); err != nil {
		return err
	}
	query := `SELECT pg_advisory_xact_lock_shared(19450001,hashtext($1))`
	if exclusive {
		query = `SELECT pg_advisory_xact_lock(19450001,hashtext($1))`
	}
	_, err := tx.Exec(ctx, query, workspace)
	return err
}

// Check revalidates admission while the coordination lock is held. A zero
// generation denotes a trusted in-process caller (fixtures/operators), never a
// browser-supplied field. Session adapters always supply a positive generation.
func Check(ctx context.Context, tx pgx.Tx, p authz.Principal) error {
	if err := p.ValidateUser(); err != nil {
		return err
	}
	var account, status string
	var revision int64
	err := tx.QueryRow(ctx, `SELECT status FROM radishnexus.user_accounts WHERE user_id=$1 FOR SHARE`, p.ID).Scan(&account)
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.ErrNotFound
	}
	if err == nil && account != "active" {
		return authn.ErrInvalidSession
	}
	if err != nil {
		return err
	}
	if p.SessionID != "" {
		var valid bool
		err = tx.QueryRow(ctx, `SELECT user_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() FROM radishnexus.user_sessions WHERE id=$1 FOR SHARE`, p.SessionID, p.ID).Scan(&valid)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !valid {
			return authn.ErrInvalidSession
		}
		if err != nil {
			return err
		}
	}
	// Lock the row even under the gate: a repeatable-read transaction may have
	// acquired its snapshot while waiting for the gate. PostgreSQL must reject
	// that old snapshot after a lifecycle change. Permission writers hold the
	// exclusive gate before admission, so no concurrent lock upgrade is possible.
	err = tx.QueryRow(ctx, `SELECT status,lifecycle_revision FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, p.WorkspaceID, p.ID).Scan(&status, &revision)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (status != "active" || p.MembershipRevision != 0 && p.MembershipRevision != revision) {
		return authz.ErrNotFound
	}
	return err
}

func Rollback(ctx context.Context, tx pgx.Tx) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := tx.Rollback(cleanup)
	if errors.Is(err, pgx.ErrTxClosed) {
		return nil
	}
	return err
}

func SchemaLock(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='5s'`); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(739140967704144)`)
	return err
}
