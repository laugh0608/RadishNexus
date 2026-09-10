package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
)

// EmailMapping is operator-supplied input, never an API profile projection.
type EmailMapping struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}

// MapLegacyEmails changes only previously unmapped credentials. The whole
// batch is atomic: invalid, duplicate or already-mapped entries roll it back.
func (store *Store) MapLegacyEmails(ctx context.Context, mappings []EmailMapping) (err error) {
	if len(mappings) == 0 || len(mappings) > 1000 {
		return errors.New("email mapping requires 1-1000 records")
	}
	canonical := make([]EmailMapping, len(mappings))
	users, emails := map[string]bool{}, map[string]bool{}
	for i, mapping := range mappings {
		email, err := authn.NormalizeEmail(mapping.Email)
		if err != nil || mapping.UserID == "" || users[mapping.UserID] || emails[email] {
			return errors.New("invalid or duplicate email mapping")
		}
		users[mapping.UserID], emails[email] = true, true
		canonical[i] = EmailMapping{UserID: mapping.UserID, Email: email}
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin email mapping: %w", err)
	}
	defer func() {
		rollbackErr := tx.Rollback(context.WithoutCancel(ctx))
		if err == nil && rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = fmt.Errorf("rollback email mapping: %w", rollbackErr)
		}
	}()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapLockID); err != nil {
		return fmt.Errorf("lock email mapping: %w", err)
	}
	for _, mapping := range canonical {
		tag, err := tx.Exec(ctx, `UPDATE radishnexus.local_credentials SET email = $2 WHERE user_id = $1 AND email IS NULL AND legacy_login_name IS NOT NULL`, mapping.UserID, mapping.Email)
		// PostgreSQL uniqueness errors can carry the private email in Detail.
		if err != nil {
			return storeError("email mapping failed; verify account and email uniqueness", err)
		}
		if tag.RowsAffected() != 1 {
			return errors.New("email mapping requires an existing unmapped legacy credential")
		}
		if _, err := tx.Exec(ctx, `UPDATE radishnexus.user_sessions SET revoked_at = greatest($2::timestamptz, created_at) WHERE user_id = $1 AND revoked_at IS NULL`, mapping.UserID, time.Now().UTC()); err != nil {
			return storeError("revoke sessions for email mapping", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return storeError("commit email mapping", err)
	}
	return nil
}
