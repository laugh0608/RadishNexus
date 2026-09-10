package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func (store *Store) BeginOIDC(ctx context.Context, record authn.OIDCTransaction) error {
	return store.identityTransaction(ctx, func(tx pgx.Tx) error {
		if len(record.InitiatingSessionDigest) != 0 {
			if _, err := sessionAccount(ctx, tx, record.InitiatingSessionDigest, record.CreatedAt, true); err != nil {
				return err
			}
		}
		if len(record.InvitationDigest) != 0 {
			if _, err := invitationWorkspace(ctx, tx, record.InvitationDigest, record.CreatedAt); err != nil {
				return err
			}
		}
		// Serialize the bounded, short state-store admission only. No upstream
		// network exchange occurs inside this transaction.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapLockID+1); err != nil {
			return storeError("lock OIDC capacity", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM radishnexus.oidc_transactions WHERE expires_at <= $1`, record.CreatedAt); err != nil {
			return storeError("purge expired OIDC state", err)
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM radishnexus.oidc_transactions`).Scan(&count); err != nil {
			return storeError("count OIDC transactions", err)
		}
		if count >= 4096 {
			return authn.ErrOIDCUnavailable
		}
		if _, err := tx.Exec(ctx, `INSERT INTO radishnexus.oidc_transactions (state_digest,browser_digest,nonce_digest,provider_digest,initiating_session_digest,invitation_digest,display_name,created_at,expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, record.StateDigest, record.BrowserDigest, record.NonceDigest, record.ProviderDigest, record.InitiatingSessionDigest, record.InvitationDigest, record.DisplayName, record.CreatedAt, record.ExpiresAt); err != nil {
			return storeError("create OIDC transaction", err)
		}
		return nil
	})
}

func (store *Store) ConsumeOIDC(ctx context.Context, state, browser []byte, now time.Time) (record authn.OIDCTransaction, err error) {
	err = store.pool.QueryRow(ctx, `DELETE FROM radishnexus.oidc_transactions WHERE state_digest = $1 AND browser_digest = $2 AND expires_at > $3 AND created_at <= $3 RETURNING state_digest,browser_digest,nonce_digest,provider_digest,initiating_session_digest,invitation_digest,display_name,created_at,expires_at`, state, browser, now).Scan(&record.StateDigest, &record.BrowserDigest, &record.NonceDigest, &record.ProviderDigest, &record.InitiatingSessionDigest, &record.InvitationDigest, &record.DisplayName, &record.CreatedAt, &record.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return record, authn.ErrOIDCInvalid
	}
	if err != nil {
		return record, storeError("consume OIDC transaction", err)
	}
	return record, nil
}

func (store *Store) CompleteOIDC(ctx context.Context, record authn.OIDCCompletion) error {
	return store.identityTransaction(ctx, func(tx pgx.Tx) error {
		now := record.Session.CreatedAt
		// The code exchange cannot extend the five-minute authorization window.
		if !now.Before(record.Transaction.ExpiresAt) {
			return authn.ErrOIDCInvalid
		}
		if len(record.Transaction.InitiatingSessionDigest) != 0 {
			userID, err := sessionAccount(ctx, tx, record.Transaction.InitiatingSessionDigest, now, true)
			if err != nil {
				return err
			}
			if err := insertExternal(ctx, tx, record.Identity, userID, now); err != nil {
				return err
			}
			return recordAudit(ctx, tx, record.AuditID, "external.linked", userID, "", now)
		}
		var userID string
		err := tx.QueryRow(ctx, `SELECT user_id FROM radishnexus.external_identities WHERE issuer = $1 AND subject = $2`, record.Identity.Issuer, record.Identity.Subject).Scan(&userID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return storeError("find external identity", err)
		}
		newAccount := errors.Is(err, pgx.ErrNoRows)
		workspaceID := ""
		if newAccount && len(record.Transaction.InvitationDigest) == 0 {
			return authn.ErrInvitationInvalid
		}
		if !newAccount {
			var status string
			if err := tx.QueryRow(ctx, `SELECT status FROM radishnexus.user_accounts WHERE user_id = $1 FOR UPDATE`, userID).Scan(&status); err != nil {
				return storeError("lock external account", err)
			}
			if status != "active" {
				return authn.ErrInvalidCredentials
			}
			var stillBound bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM radishnexus.external_identities WHERE issuer = $1 AND subject = $2 AND user_id = $3)`, record.Identity.Issuer, record.Identity.Subject, userID).Scan(&stillBound); err != nil {
				return storeError("recheck external identity", err)
			}
			if !stillBound {
				return authn.ErrOIDCInvalid
			}
		}
		if len(record.Transaction.InvitationDigest) != 0 {
			var err error
			workspaceID, err = invitationWorkspace(ctx, tx, record.Transaction.InvitationDigest, now)
			if err != nil {
				return err
			}
		}
		if newAccount {
			userID = record.NewUserID
			if record.Transaction.DisplayName == "" {
				return authz.ErrInvalid
			}
			if err := insertAccount(ctx, tx, userID, record.Transaction.DisplayName, now); err != nil {
				return err
			}
			if err := insertExternal(ctx, tx, record.Identity, userID, now); err != nil {
				return err
			}
		}
		if workspaceID != "" {
			if err := joinInvitation(ctx, tx, record.Transaction.InvitationDigest, workspaceID, userID, now); err != nil {
				return err
			}
		}
		record.Session.UserID = userID
		if err := insertSession(ctx, tx, record.Session); err != nil {
			return err
		}
		return recordAudit(ctx, tx, record.AuditID, "external.login", userID, workspaceID, now)
	})
}

func insertExternal(ctx context.Context, tx pgx.Tx, identity authn.ExternalIdentity, userID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `INSERT INTO radishnexus.external_identities (user_id,issuer,subject,created_at) VALUES ($1,$2,$3,$4)`, userID, identity.Issuer, identity.Subject, now); err != nil {
		var state interface{ SQLState() string }
		if errors.As(err, &state) && state.SQLState() == "23505" {
			return authn.ErrIdentityConflict
		}
		return storeError("bind external identity", err)
	}
	return nil
}
