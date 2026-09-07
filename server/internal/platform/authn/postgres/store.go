// Package postgres persists local identities and opaque user sessions.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const bootstrapLockID = int64(739140967704145)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (store *Store) Bootstrap(ctx context.Context, record authn.BootstrapRecord) (err error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin local identity bootstrap: %w", err)
	}
	defer func() {
		rollbackErr := tx.Rollback(context.WithoutCancel(ctx))
		if err == nil && rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = fmt.Errorf("rollback local identity bootstrap: %w", rollbackErr)
		}
	}()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", bootstrapLockID); err != nil {
		return fmt.Errorf("lock local identity bootstrap: %w", err)
	}
	var alreadyBootstrapped bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM radishnexus.local_accounts)`).Scan(&alreadyBootstrapped); err != nil {
		return fmt.Errorf("inspect local identity bootstrap state: %w", err)
	}
	if alreadyBootstrapped {
		return authn.ErrAlreadyBootstrapped
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO radishnexus.users (id, display_name, created_at)
		VALUES ($1, $2, $3)
	`, record.UserID, record.DisplayName, record.CreatedAt); err != nil {
		return fmt.Errorf("insert bootstrap user: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO radishnexus.workspaces (id, name, created_at)
		VALUES ($1, $2, $3)
	`, record.WorkspaceID, record.WorkspaceName, record.CreatedAt); err != nil {
		return fmt.Errorf("insert bootstrap workspace: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO radishnexus.workspace_memberships (
			workspace_id, user_id, status, role, created_at
		) VALUES ($1, $2, 'active', 'owner', $3)
	`, record.WorkspaceID, record.UserID, record.CreatedAt); err != nil {
		return fmt.Errorf("insert bootstrap workspace owner: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO radishnexus.local_accounts (
			user_id, login_name, password_hash, status, created_at, password_changed_at
		) VALUES ($1, $2, $3, 'active', $4, $4)
	`, record.UserID, record.LoginName, record.PasswordHash, record.CreatedAt); err != nil {
		return fmt.Errorf("insert bootstrap local account: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit local identity bootstrap: %w", err)
	}
	return nil
}

func (store *Store) FindLocalAccount(ctx context.Context, loginName string) (authn.LocalAccount, error) {
	var account authn.LocalAccount
	err := store.pool.QueryRow(ctx, `
		SELECT user_id, password_hash, status, locked_until
		FROM radishnexus.local_accounts
		WHERE login_name = $1
	`, loginName).Scan(
		&account.UserID,
		&account.PasswordHash,
		&account.Status,
		&account.LockedUntil,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return authn.LocalAccount{}, authn.ErrAccountNotFound
	}
	if err != nil {
		return authn.LocalAccount{}, fmt.Errorf("find local account: %w", err)
	}
	return account, nil
}

func (store *Store) RecordFailedLogin(
	ctx context.Context,
	userID string,
	occurredAt time.Time,
	lockUntil time.Time,
	failureLimit int,
) error {
	_, err := store.pool.Exec(ctx, `
		UPDATE radishnexus.local_accounts
		SET failed_login_count = CASE
				WHEN failed_login_count + 1 >= $4 THEN 0
				ELSE failed_login_count + 1
			END,
			locked_until = CASE
				WHEN failed_login_count + 1 >= $4 THEN $3
				ELSE locked_until
			END
		WHERE user_id = $1
		  AND status = 'active'
		  AND (locked_until IS NULL OR locked_until <= $2)
	`, userID, occurredAt, lockUntil, failureLimit)
	if err != nil {
		return fmt.Errorf("record failed local login: %w", err)
	}
	return nil
}

func (store *Store) CreateSession(ctx context.Context, record authn.SessionRecord) (err error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin user session creation: %w", err)
	}
	defer func() {
		rollbackErr := tx.Rollback(context.WithoutCancel(ctx))
		if err == nil && rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = fmt.Errorf("rollback user session creation: %w", rollbackErr)
		}
	}()

	commandTag, err := tx.Exec(ctx, `
		UPDATE radishnexus.local_accounts
		SET failed_login_count = 0,
			locked_until = NULL,
			last_authenticated_at = $3
		WHERE user_id = $1
		  AND password_hash = $2
		  AND status = 'active'
		  AND (locked_until IS NULL OR locked_until <= $3)
	`, record.UserID, record.PasswordHash, record.CreatedAt)
	if err != nil {
		return fmt.Errorf("confirm local account for session: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return authn.ErrInvalidCredentials
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO radishnexus.user_sessions (
			id, user_id, token_digest, csrf_token_digest, created_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`,
		record.ID,
		record.UserID,
		record.TokenDigest,
		record.CSRFTokenDigest,
		record.CreatedAt,
		record.ExpiresAt,
	); err != nil {
		return fmt.Errorf("insert user session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit user session creation: %w", err)
	}
	return nil
}

func (store *Store) ResolveSession(
	ctx context.Context,
	tokenDigest []byte,
	now time.Time,
) (resolved authn.ResolvedSession, err error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return authn.ResolvedSession{}, fmt.Errorf("begin user session resolution: %w", err)
	}
	defer func() {
		rollbackErr := tx.Rollback(context.WithoutCancel(ctx))
		if err == nil && rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = fmt.Errorf("rollback user session resolution: %w", rollbackErr)
		}
	}()

	err = tx.QueryRow(ctx, `
		SELECT session.csrf_token_digest, session.expires_at,
		       users.id, users.display_name
		FROM radishnexus.user_sessions AS session
		JOIN radishnexus.local_accounts AS account ON account.user_id = session.user_id
		JOIN radishnexus.users AS users ON users.id = session.user_id
		WHERE session.token_digest = $1
		  AND session.revoked_at IS NULL
		  AND session.expires_at > $2
		  AND account.status = 'active'
	`, tokenDigest, now).Scan(
		&resolved.CSRFTokenDigest,
		&resolved.Account.ExpiresAt,
		&resolved.Account.User.ID,
		&resolved.Account.User.DisplayName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return authn.ResolvedSession{}, authn.ErrInvalidSession
	}
	if err != nil {
		return authn.ResolvedSession{}, fmt.Errorf("resolve user session: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT workspaces.id, workspaces.name, membership.role
		FROM radishnexus.workspace_memberships AS membership
		JOIN radishnexus.workspaces AS workspaces ON workspaces.id = membership.workspace_id
		WHERE membership.user_id = $1 AND membership.status = 'active'
		ORDER BY workspaces.id
	`, resolved.Account.User.ID)
	if err != nil {
		return authn.ResolvedSession{}, fmt.Errorf("list session workspaces: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var workspace authn.Workspace
		if err := rows.Scan(&workspace.ID, &workspace.Name, &workspace.Role); err != nil {
			return authn.ResolvedSession{}, fmt.Errorf("scan session workspace: %w", err)
		}
		resolved.Account.Workspaces = append(resolved.Account.Workspaces, workspace)
	}
	if err := rows.Err(); err != nil {
		return authn.ResolvedSession{}, fmt.Errorf("iterate session workspaces: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return authn.ResolvedSession{}, fmt.Errorf("commit user session resolution: %w", err)
	}
	return resolved, nil
}

func (store *Store) ResolveWorkspaceSession(
	ctx context.Context,
	tokenDigest []byte,
	workspaceID string,
	now time.Time,
) (authn.VerifiedUser, error) {
	var userID string
	err := store.pool.QueryRow(ctx, `
		SELECT session.user_id
		FROM radishnexus.user_sessions AS session
		JOIN radishnexus.local_accounts AS account ON account.user_id = session.user_id
		JOIN radishnexus.workspace_memberships AS membership
		  ON membership.user_id = session.user_id
		 AND membership.workspace_id = $2
		 AND membership.status = 'active'
		WHERE session.token_digest = $1
		  AND session.revoked_at IS NULL
		  AND session.expires_at > $3
		  AND account.status = 'active'
	`, tokenDigest, workspaceID, now).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, sessionErr := store.ResolveSession(ctx, tokenDigest, now); sessionErr != nil {
			return authn.VerifiedUser{}, sessionErr
		}
		return authn.VerifiedUser{}, authz.ErrForbidden
	}
	if err != nil {
		return authn.VerifiedUser{}, fmt.Errorf("resolve workspace session: %w", err)
	}
	return authn.VerifiedUser{UserID: userID, WorkspaceID: workspaceID}, nil
}

func (store *Store) RevokeSession(ctx context.Context, tokenDigest []byte, now time.Time) error {
	commandTag, err := store.pool.Exec(ctx, `
		UPDATE radishnexus.user_sessions
		SET revoked_at = $2
		WHERE token_digest = $1
		  AND revoked_at IS NULL
		  AND expires_at > $2
	`, tokenDigest, now)
	if err != nil {
		return fmt.Errorf("revoke user session: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return authn.ErrInvalidSession
	}
	return nil
}
