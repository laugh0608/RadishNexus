package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

// Keep the cause available to errors.Is/As without printing PostgreSQL Detail,
// which can contain a private email, credential or token supplied in a query.
type privateStoreError struct {
	operation string
	cause     error
}

func (err privateStoreError) Error() string {
	var state interface{ SQLState() string }
	if errors.As(err.cause, &state) {
		return err.operation + ": SQLSTATE " + state.SQLState()
	}
	return err.operation + ": database operation failed"
}
func (err privateStoreError) Unwrap() error        { return err.cause }
func storeError(operation string, err error) error { return privateStoreError{operation, err} }

func (store *Store) identityTransaction(ctx context.Context, action func(pgx.Tx) error) (err error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return storeError("begin identity transaction", err)
	}
	defer func() {
		rollbackErr := tx.Rollback(context.WithoutCancel(ctx))
		if err == nil && rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = storeError("rollback identity transaction", rollbackErr)
		}
	}()
	if err := action(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return storeError("commit identity transaction", err)
	}
	return nil
}

// Lock account state before login-method mutation. Re-read the session under
// that lock, so a concurrent unlink/revoke cannot authorize a later mutation.
func sessionAccount(ctx context.Context, tx pgx.Tx, digest []byte, now time.Time, recent bool) (string, error) {
	var userID, status string
	if err := tx.QueryRow(ctx, `SELECT account.user_id, account.status FROM radishnexus.user_accounts AS account JOIN radishnexus.user_sessions AS session ON session.user_id = account.user_id WHERE session.token_digest = $1 FOR UPDATE OF account`, digest).Scan(&userID, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", authn.ErrInvalidSession
		}
		return "", storeError("lock session account", err)
	}
	if status != "active" {
		return "", authn.ErrInvalidSession
	}
	var created time.Time
	if err := tx.QueryRow(ctx, `SELECT created_at FROM radishnexus.user_sessions WHERE token_digest = $1 AND revoked_at IS NULL AND expires_at > $2`, digest, now).Scan(&created); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", authn.ErrInvalidSession
		}
		return "", storeError("recheck session", err)
	}
	if recent && (created.Before(now.Add(-authn.RecentAuthentication)) || created.After(now)) {
		return "", authn.ErrRecentAuthentication
	}
	return userID, nil
}

func (store *Store) AccountDetails(ctx context.Context, digest []byte, issuer string, now time.Time) (details authn.AccountDetails, err error) {
	err = store.pool.QueryRow(ctx, `
        SELECT users.id, users.display_name,
            EXISTS (SELECT 1 FROM radishnexus.local_credentials WHERE user_id = users.id AND email IS NOT NULL AND status = 'active'),
            EXISTS (SELECT 1 FROM radishnexus.external_identities WHERE user_id = users.id AND issuer = $2),
            session.created_at >= $3::timestamptz - interval '10 minutes' AND session.created_at <= $3
        FROM radishnexus.user_sessions AS session
        JOIN radishnexus.user_accounts AS account ON account.user_id = session.user_id AND account.status = 'active'
        JOIN radishnexus.users AS users ON users.id = account.user_id
        WHERE session.token_digest = $1 AND session.revoked_at IS NULL AND session.expires_at > $3
    `, digest, issuer, now).Scan(&details.User.ID, &details.User.DisplayName, &details.HasPassword, &details.RadishLinked, &details.RecentlyAuthenticated)
	if errors.Is(err, pgx.ErrNoRows) {
		return details, authn.ErrInvalidSession
	}
	if err != nil {
		return details, storeError("read account details", err)
	}
	return details, nil
}

func requireOwner(ctx context.Context, tx pgx.Tx, userID, workspaceID string) error {
	var status, role string
	if err := tx.QueryRow(ctx, `SELECT status, role FROM radishnexus.workspace_memberships WHERE workspace_id = $1 AND user_id = $2 FOR SHARE`, workspaceID, userID).Scan(&status, &role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return authz.ErrNotFound
		}
		return storeError("read invitation owner membership", err)
	}
	if status != "active" || role != "owner" {
		return authz.ErrForbidden
	}
	return nil
}

func (store *Store) CreateInvitation(ctx context.Context, digest []byte, workspaceID string, record authn.InvitationRecord) error {
	return store.identityTransaction(ctx, func(tx pgx.Tx) error {
		userID, err := sessionAccount(ctx, tx, digest, record.CreatedAt, false)
		if err != nil {
			return err
		}
		if err := requireOwner(ctx, tx, userID, workspaceID); err != nil {
			return err
		}
		// Bound outstanding admission capabilities per owner/workspace. Expired
		// and consumed capabilities carry no audit authority and may be purged.
		if _, err := tx.Exec(ctx, `DELETE FROM radishnexus.identity_invitations WHERE expires_at <= $1 OR consumed_at IS NOT NULL`, record.CreatedAt); err != nil {
			return storeError("purge inactive invitations", err)
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM radishnexus.identity_invitations WHERE workspace_id = $1 AND created_by = $2`, workspaceID, userID).Scan(&count); err != nil {
			return storeError("count invitations", err)
		}
		if count >= 20 {
			return authz.ErrConflict
		}
		if _, err := tx.Exec(ctx, `INSERT INTO radishnexus.identity_invitations (id,token_digest,workspace_id,created_by,created_at,expires_at) VALUES ($1,$2,$3,$4,$5,$6)`, record.ID, record.TokenDigest, workspaceID, userID, record.CreatedAt, record.ExpiresAt); err != nil {
			return storeError("create invitation", err)
		}
		return recordAudit(ctx, tx, record.AuditID, "invitation.created", userID, workspaceID, record.CreatedAt)
	})
}

func invitationWorkspace(ctx context.Context, tx pgx.Tx, digest []byte, now time.Time) (string, error) {
	var workspaceID, creatorID string
	if err := tx.QueryRow(ctx, `SELECT workspace_id, created_by FROM radishnexus.identity_invitations WHERE token_digest = $1 AND consumed_at IS NULL AND expires_at > $2 AND created_at <= $2 FOR UPDATE`, digest, now).Scan(&workspaceID, &creatorID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", authn.ErrInvitationInvalid
		}
		return "", storeError("read admission invitation", err)
	}
	var creatorStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM radishnexus.user_accounts WHERE user_id = $1 FOR SHARE`, creatorID).Scan(&creatorStatus); err != nil {
		return "", storeError("check invitation creator", err)
	}
	if creatorStatus != "active" {
		return "", authn.ErrInvitationInvalid
	}
	if err := requireOwner(ctx, tx, creatorID, workspaceID); err != nil {
		if errors.Is(err, authz.ErrNotFound) || errors.Is(err, authz.ErrForbidden) {
			return "", authn.ErrInvitationInvalid
		}
		return "", err
	}
	return workspaceID, nil
}

func joinInvitation(ctx context.Context, tx pgx.Tx, digest []byte, workspaceID, userID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `INSERT INTO radishnexus.workspace_memberships (workspace_id,user_id,status,role,created_at) VALUES ($1,$2,'active','member',$3) ON CONFLICT (workspace_id,user_id) DO NOTHING`, workspaceID, userID, now); err != nil {
		return storeError("join invited workspace", err)
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM radishnexus.workspace_memberships WHERE workspace_id = $1 AND user_id = $2 FOR SHARE`, workspaceID, userID).Scan(&status); err != nil {
		return storeError("check invited membership", err)
	}
	if status != "active" {
		return authz.ErrForbidden
	}
	if _, err := tx.Exec(ctx, `UPDATE radishnexus.identity_invitations SET consumed_at = $2 WHERE token_digest = $1`, digest, now); err != nil {
		return storeError("consume invitation", err)
	}
	return nil
}

func insertAccount(ctx context.Context, tx pgx.Tx, userID, displayName string, now time.Time) error {
	if _, err := tx.Exec(ctx, `INSERT INTO radishnexus.users (id,display_name,created_at) VALUES ($1,$2,$3)`, userID, displayName, now); err != nil {
		return storeError("create invited user", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO radishnexus.user_accounts (user_id,status,created_at) VALUES ($1,'active',$2)`, userID, now); err != nil {
		return storeError("create invited account", err)
	}
	return nil
}

func insertSession(ctx context.Context, tx pgx.Tx, record authn.SessionRecord) error {
	if _, err := tx.Exec(ctx, `INSERT INTO radishnexus.user_sessions (id,user_id,token_digest,csrf_token_digest,created_at,expires_at) VALUES ($1,$2,$3,$4,$5,$6)`, record.ID, record.UserID, record.TokenDigest, record.CSRFTokenDigest, record.CreatedAt, record.ExpiresAt); err != nil {
		return storeError("create admitted session", err)
	}
	return nil
}

func (store *Store) AcceptInvitation(ctx context.Context, record authn.InvitationAcceptance) (userID string, err error) {
	err = store.identityTransaction(ctx, func(tx pgx.Tx) error {
		userID = record.UserID
		if len(record.ExistingSessionDigest) != 0 {
			var err error
			userID, err = sessionAccount(ctx, tx, record.ExistingSessionDigest, record.Session.CreatedAt, false)
			if err != nil {
				return err
			}
		}
		workspaceID, err := invitationWorkspace(ctx, tx, record.InvitationDigest, record.Session.CreatedAt)
		if err != nil {
			return err
		}
		if len(record.ExistingSessionDigest) == 0 {
			if err := insertAccount(ctx, tx, userID, record.DisplayName, record.Session.CreatedAt); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO radishnexus.local_credentials (user_id,email,password_hash,status,created_at,password_changed_at) VALUES ($1,$2,$3,'active',$4,$4)`, userID, record.Email, record.PasswordHash, record.Session.CreatedAt); err != nil {
				var state interface{ SQLState() string }
				if errors.As(err, &state) && state.SQLState() == "23505" {
					return authn.ErrIdentityConflict
				}
				return storeError("create invited password credential", err)
			}
			if err := insertSession(ctx, tx, record.Session); err != nil {
				return err
			}
		}
		if err := joinInvitation(ctx, tx, record.InvitationDigest, workspaceID, userID, record.Session.CreatedAt); err != nil {
			return err
		}
		return recordAudit(ctx, tx, record.AuditID, "invitation.accepted", userID, workspaceID, record.Session.CreatedAt)
	})
	return userID, err
}

func (store *Store) UnlinkExternal(ctx context.Context, digest []byte, issuer, auditID string, now time.Time) error {
	return store.identityTransaction(ctx, func(tx pgx.Tx) error {
		userID, err := sessionAccount(ctx, tx, digest, now, true)
		if err != nil {
			return err
		}
		var hasPassword bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM radishnexus.local_credentials WHERE user_id = $1 AND email IS NOT NULL AND status = 'active' AND (locked_until IS NULL OR locked_until <= $2))`, userID, now).Scan(&hasPassword); err != nil {
			return storeError("check remaining login method", err)
		}
		if !hasPassword {
			return authn.ErrLastLoginMethod
		}
		tag, err := tx.Exec(ctx, `DELETE FROM radishnexus.external_identities WHERE user_id = $1 AND issuer = $2`, userID, issuer)
		if err != nil {
			return storeError("unlink external identity", err)
		}
		if tag.RowsAffected() != 1 {
			return authz.ErrNotFound
		}
		if _, err := tx.Exec(ctx, `UPDATE radishnexus.user_sessions SET revoked_at = greatest($2::timestamptz,created_at) WHERE user_id = $1 AND revoked_at IS NULL`, userID, now); err != nil {
			return storeError("revoke sessions after unlink", err)
		}
		return recordAudit(ctx, tx, auditID, "external.unlinked", userID, "", now)
	})
}

func recordAudit(ctx context.Context, tx pgx.Tx, id, action, userID, workspaceID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `INSERT INTO radishnexus.identity_audit (id,action,actor_id,workspace_id,occurred_at) VALUES ($1,$2,$3,nullif($4,''),$5)`, id, action, userID, workspaceID, now); err != nil {
		return storeError(fmt.Sprintf("record identity audit %s", action), err)
	}
	return nil
}
