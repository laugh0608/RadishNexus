package postgres

import (
	"context"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

// Keep SQL causes classifiable without printing parameter values or Detail.
type ownerStoreError struct {
	operation string
	cause     error
}

func (e ownerStoreError) Error() string              { return e.operation + ": database operation failed" }
func (e ownerStoreError) Unwrap() error              { return e.cause }
func ownerDBError(operation string, err error) error { return ownerStoreError{operation, err} }

func (s *Store) ownerTransaction(ctx context.Context, action func(pgx.Tx) (bool, error)) (err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return ownerDBError("begin owner transfer transaction", err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if e := tx.Rollback(rollbackCtx); e != nil && !errors.Is(e, pgx.ErrTxClosed) {
			err = errors.Join(err, ownerDBError("rollback owner transfer transaction", e))
		}
	}()
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='5s'`); err != nil {
		return ownerDBError("set owner transfer lock budget", err)
	}
	changed, err := action(tx)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		var pgerr *pgconn.PgError
		if changed && !errors.Is(err, pgx.ErrTxCommitRollback) && !(errors.As(err, &pgerr) && pgerr.Severity == "ERROR") {
			return errors.Join(goldenpath.ErrOwnerTransferUnconfirmed, ownerDBError("owner transfer commit acknowledgement", err))
		}
		return ownerDBError("commit owner transfer transaction", err)
	}
	return nil
}

type ownerSessionState struct{ created, expires time.Time }
type ownerMembership struct {
	status, role string
	revision     int64
}

func lockOwnerAccount(ctx context.Context, tx pgx.Tx, id string) (bool, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM radishnexus.user_accounts WHERE user_id=$1 FOR SHARE`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, ownerDBError("lock owner account", err)
	}
	return status == "active", nil
}
func lockOwnerSession(ctx context.Context, tx pgx.Tx, a goldenpath.OwnerAccess) (ownerSessionState, error) {
	var session ownerSessionState
	var user string
	var revoked *time.Time
	err := tx.QueryRow(ctx, `SELECT user_id,created_at,expires_at,revoked_at FROM radishnexus.user_sessions WHERE token_digest=$1 FOR SHARE`, a.SessionDigest[:]).Scan(&user, &session.created, &session.expires, &revoked)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (user != a.Principal.ID || revoked != nil)) {
		return session, authn.ErrInvalidSession
	}
	if err != nil {
		return session, ownerDBError("lock owner session", err)
	}
	return session, nil
}
func ownerRecentlyAuthenticated(created, now time.Time) bool {
	return !created.Before(now.Add(-authn.RecentAuthentication))
}
func ownerSessionNow(ctx context.Context, tx pgx.Tx, session ownerSessionState, recent bool) (time.Time, error) {
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return now, ownerDBError("read owner transfer clock", err)
	}
	if session.created.After(now) || !session.expires.After(now) {
		return now, authn.ErrInvalidSession
	}
	if recent && !ownerRecentlyAuthenticated(session.created, now) {
		return now, authn.ErrRecentAuthentication
	}
	return now, nil
}
func lockOwnerMembership(ctx context.Context, tx pgx.Tx, workspace, user string, write bool) (ownerMembership, error) {
	var m ownerMembership
	lock := "FOR SHARE"
	if write {
		lock = "FOR UPDATE"
	}
	err := tx.QueryRow(ctx, `SELECT status,role,role_revision FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2 `+lock, workspace, user).Scan(&m.status, &m.role, &m.revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, nil
	}
	if err != nil {
		return m, ownerDBError("lock owner membership", err)
	}
	return m, nil
}
func lockOwnerReader(ctx context.Context, tx pgx.Tx, a goldenpath.OwnerAccess) (ownerSessionState, ownerMembership, error) {
	active, err := lockOwnerAccount(ctx, tx, a.Principal.ID)
	if err != nil {
		return ownerSessionState{}, ownerMembership{}, err
	}
	if !active {
		return ownerSessionState{}, ownerMembership{}, authn.ErrInvalidSession
	}
	session, err := lockOwnerSession(ctx, tx, a)
	if err != nil {
		return session, ownerMembership{}, err
	}
	m, err := lockOwnerMembership(ctx, tx, a.Principal.WorkspaceID, a.Principal.ID, false)
	if err != nil {
		return session, m, err
	}
	if _, err = ownerSessionNow(ctx, tx, session, false); err != nil {
		return session, m, err
	}
	if m.status != "active" {
		return session, m, authz.ErrNotFound
	}
	return session, m, nil
}
func (s *Store) ReadOwnership(ctx context.Context, a goldenpath.OwnerAccess) (out goldenpath.Ownership, err error) {
	err = s.ownerTransaction(ctx, func(tx pgx.Tx) (bool, error) {
		session, m, e := lockOwnerReader(ctx, tx, a)
		if e != nil {
			return false, e
		}
		now, e := ownerSessionNow(ctx, tx, session, false)
		if e != nil {
			return false, e
		}
		out = goldenpath.Ownership{UserID: a.Principal.ID, Role: m.role, RoleRevision: m.revision, RecentlyAuthenticated: ownerRecentlyAuthenticated(session.created, now)}
		return false, nil
	})
	return
}
func (s *Store) ListOwnerCandidates(ctx context.Context, a goldenpath.OwnerAccess, after string, limit int) (out goldenpath.OwnerCandidatePage, err error) {
	out.Items = []goldenpath.OwnerCandidate{}
	err = s.ownerTransaction(ctx, func(tx pgx.Tx) (bool, error) {
		session, m, e := lockOwnerReader(ctx, tx, a)
		if e != nil {
			return false, e
		}
		if m.role != "owner" {
			return false, authz.ErrForbidden
		}
		rows, e := tx.Query(ctx, `SELECT u.id,u.display_name,m.role_revision FROM radishnexus.workspace_memberships m JOIN radishnexus.users u ON u.id=m.user_id JOIN radishnexus.user_accounts a ON a.user_id=u.id WHERE m.workspace_id=$1 AND m.status='active' AND m.role='member' AND a.status='active' AND u.id<>$2 AND u.id>$3 ORDER BY u.id LIMIT $4`, a.Principal.WorkspaceID, a.Principal.ID, after, limit+1)
		if e != nil {
			return false, ownerDBError("list owner candidates", e)
		}
		for rows.Next() {
			var candidate goldenpath.OwnerCandidate
			if e = rows.Scan(&candidate.ID, &candidate.DisplayName, &candidate.RoleRevision); e != nil {
				rows.Close()
				return false, ownerDBError("scan owner candidate", e)
			}
			out.Items = append(out.Items, candidate)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return false, ownerDBError("read owner candidates", e)
		}
		if len(out.Items) > limit {
			out.Items = out.Items[:limit]
			out.NextID = out.Items[limit-1].ID
		}
		_, e = ownerSessionNow(ctx, tx, session, false)
		return false, e
	})
	return
}
func ownerReceipt(ctx context.Context, tx pgx.Tx, a goldenpath.OwnerAccess, operation string) (out goldenpath.OwnerTransferResult, digest string, err error) {
	out = goldenpath.OwnerTransferResult{Result: "not_observed", WorkspaceID: a.Principal.WorkspaceID, ClientOperationID: operation}
	err = tx.QueryRow(ctx, `SELECT r.payload_sha256,a.actor_id,a.subject_id,a.id,a.occurred_at,a.actor_after_role_revision,a.subject_after_role_revision FROM radishnexus.workspace_configuration_receipts r JOIN radishnexus.workspace_configuration_audit a ON a.workspace_id=r.workspace_id AND a.id=r.audit_id WHERE r.workspace_id=$1 AND r.actor_id=$2 AND r.command_kind='workspace.owner.transfer' AND r.client_operation_id=$3`, a.Principal.WorkspaceID, a.Principal.ID, operation).Scan(&digest, &out.ActorUserID, &out.TargetUserID, &out.AuditID, &out.OccurredAt, &out.ActorRoleRevision, &out.TargetRoleRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, "", nil
	}
	if err != nil {
		return goldenpath.OwnerTransferResult{}, "", ownerDBError("read owner transfer receipt", err)
	}
	out.Result = "applied"
	return
}
func (s *Store) ReadOwnerTransfer(ctx context.Context, a goldenpath.OwnerAccess, operation string) (out goldenpath.OwnerTransferResult, err error) {
	err = s.ownerTransaction(ctx, func(tx pgx.Tx) (bool, error) {
		session, _, e := lockOwnerReader(ctx, tx, a)
		if e != nil {
			return false, e
		}
		out, _, e = ownerReceipt(ctx, tx, a, operation)
		if e != nil {
			return false, e
		}
		_, e = ownerSessionNow(ctx, tx, session, false)
		return false, e
	})
	return
}
func (s *Store) TransferOwner(ctx context.Context, c goldenpath.OwnerTransferCommand) (out goldenpath.OwnerTransferResult, err error) {
	a := c.Access
	err = s.ownerTransaction(ctx, func(tx pgx.Tx) (bool, error) {
		// A pre-read selects the history-only path. It never authorizes that read.
		previous, _, e := ownerReceipt(ctx, tx, a, c.Input.ClientOperationID)
		if e != nil {
			return false, e
		}
		if previous.Result == "applied" {
			session, _, e := lockOwnerReader(ctx, tx, a)
			if e != nil {
				return false, e
			}
			var digest string
			out, digest, e = ownerReceipt(ctx, tx, a, c.Input.ClientOperationID)
			if e != nil {
				return false, e
			}
			if digest != c.PayloadSHA256 {
				return false, authz.ErrConflict
			}
			_, e = ownerSessionNow(ctx, tx, session, false)
			return false, e
		}
		ids := []string{a.Principal.ID, c.Input.TargetUserID}
		slices.Sort(ids)
		active := map[string]bool{}
		for _, id := range ids {
			active[id], e = lockOwnerAccount(ctx, tx, id)
			if e != nil {
				return false, e
			}
		}
		if !active[a.Principal.ID] {
			return false, authn.ErrInvalidSession
		}
		session, e := lockOwnerSession(ctx, tx, a)
		if e != nil {
			return false, e
		}
		members := map[string]ownerMembership{}
		for _, id := range ids {
			members[id], e = lockOwnerMembership(ctx, tx, a.Principal.WorkspaceID, id, true)
			if e != nil {
				return false, e
			}
		}
		if _, e = ownerSessionNow(ctx, tx, session, false); e != nil {
			return false, e
		}
		actor, target := members[a.Principal.ID], members[c.Input.TargetUserID]
		if actor.status != "active" {
			return false, authz.ErrNotFound
		}
		// The first request may have committed while this one waited for membership.
		var digest string
		out, digest, e = ownerReceipt(ctx, tx, a, c.Input.ClientOperationID)
		if e != nil {
			return false, e
		}
		if out.Result == "applied" {
			if digest != c.PayloadSHA256 {
				return false, authz.ErrConflict
			}
			return false, nil
		}
		if actor.role != "owner" {
			return false, authz.ErrForbidden
		}
		if !active[c.Input.TargetUserID] || target.status != "active" {
			return false, authz.ErrNotFound
		}
		if target.role != "member" || actor.revision != c.ActorRevision || target.revision != c.TargetRevision || actor.revision == math.MaxInt64 || target.revision == math.MaxInt64 {
			return false, authz.ErrConflict
		}
		if _, e = tx.Exec(ctx, `SELECT id FROM radishnexus.workspaces WHERE id=$1 FOR UPDATE`, a.Principal.WorkspaceID); e != nil {
			return false, ownerDBError("lock owner transfer workspace", e)
		}
		out, digest, e = ownerReceipt(ctx, tx, a, c.Input.ClientOperationID)
		if e != nil {
			return false, e
		}
		if out.Result == "applied" {
			if digest != c.PayloadSHA256 {
				return false, authz.ErrConflict
			}
			_, e = ownerSessionNow(ctx, tx, session, false)
			return false, e
		}
		now, e := ownerSessionNow(ctx, tx, session, true)
		if e != nil {
			return false, e
		}
		// Promote first, then demote, in the same transaction. No observer sees half.
		for _, change := range []struct {
			id, role string
			revision int64
		}{{c.Input.TargetUserID, "owner", target.revision}, {a.Principal.ID, "member", actor.revision}} {
			tag, e := tx.Exec(ctx, `UPDATE radishnexus.workspace_memberships SET role=$3,role_revision=role_revision+1 WHERE workspace_id=$1 AND user_id=$2 AND role_revision=$4 AND status='active'`, a.Principal.WorkspaceID, change.id, change.role, change.revision)
			if e != nil {
				return false, ownerDBError("update owner role", e)
			}
			if tag.RowsAffected() != 1 {
				return false, authz.ErrConflict
			}
		}
		_, e = tx.Exec(ctx, `INSERT INTO radishnexus.workspace_configuration_audit(id,workspace_id,actor_id,command_kind,scope_id,subject_id,result_id,before_state,after_state,changed,request_id,occurred_at,actor_before_role,actor_after_role,actor_before_role_revision,actor_after_role_revision,subject_before_role_revision,subject_after_role_revision) VALUES($1,$2,$3,'workspace.owner.transfer',$2,$4,$4,'member','owner',true,$5,$6,'owner','member',$7::bigint,$7::bigint+1,$8::bigint,$8::bigint+1)`, c.AuditID, a.Principal.WorkspaceID, a.Principal.ID, c.Input.TargetUserID, c.RequestID, now, actor.revision, target.revision)
		if e != nil {
			return false, ownerDBError("record owner transfer audit", e)
		}
		_, e = tx.Exec(ctx, `INSERT INTO radishnexus.workspace_configuration_receipts(workspace_id,actor_id,command_kind,scope_id,subject_id,client_operation_id,payload_sha256,audit_id,created_at) VALUES($1,$2,'workspace.owner.transfer',$1,$3,$4,$5,$6,$7)`, a.Principal.WorkspaceID, a.Principal.ID, c.Input.TargetUserID, c.Input.ClientOperationID, c.PayloadSHA256, c.AuditID, now)
		if e != nil {
			return false, ownerDBError("record owner transfer receipt", e)
		}
		out = goldenpath.OwnerTransferResult{Result: "applied", WorkspaceID: a.Principal.WorkspaceID, ClientOperationID: c.Input.ClientOperationID, ActorUserID: a.Principal.ID, TargetUserID: c.Input.TargetUserID, AuditID: c.AuditID, OccurredAt: now, ActorRoleRevision: actor.revision + 1, TargetRoleRevision: target.revision + 1}
		return true, nil
	})
	return
}
