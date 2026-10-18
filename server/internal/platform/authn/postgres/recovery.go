package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
)

const recoveryCandidateColumns = `SELECT a.user_id, coalesce(c.email,''), a.status,
 coalesce(c.status,''), coalesce(c.password_hash,''), coalesce(c.password_changed_at,a.created_at)
 FROM radishnexus.user_accounts a LEFT JOIN radishnexus.local_credentials c ON c.user_id=a.user_id `

func scanRecoveryCandidate(row pgx.Row) (c authn.RecoveryCandidate, err error) {
	err = row.Scan(&c.UserID, &c.Email, &c.AccountStatus, &c.CredentialStatus, &c.PasswordHash, &c.PasswordChangedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, authn.ErrRecoveryIneligible
	}
	if err != nil {
		return c, storeError("read recovery candidate", err)
	}
	return c, nil
}
func (s *Store) InspectRecovery(ctx context.Context, email string) (authn.RecoveryCandidate, error) {
	return scanRecoveryCandidate(s.pool.QueryRow(ctx, recoveryCandidateColumns+` WHERE c.email=$1`, email))
}
func (s *Store) FindRecoveryCandidate(ctx context.Context, userID string) (authn.RecoveryCandidate, error) {
	return scanRecoveryCandidate(s.pool.QueryRow(ctx, recoveryCandidateColumns+` WHERE a.user_id=$1`, userID))
}
func (s *Store) RecoveryOperationExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM radishnexus.identity_audit WHERE operation_id=$1::uuid)`, id).Scan(&exists)
	if err != nil {
		return false, storeError("check recovery operation", err)
	}
	return exists, nil
}
func (s *Store) RecoveryStatus(ctx context.Context, userID, operationID string) (authn.RecoveryResult, error) {
	result := authn.RecoveryResult{Result: "not_observed", UserID: userID, OperationID: operationID}
	var when time.Time
	err := s.pool.QueryRow(ctx, `SELECT id,applied_password_changed_at FROM radishnexus.identity_audit WHERE target_user_id=$1 AND operation_id=$2::uuid AND action='local.password.recovered'`, userID, operationID).Scan(&result.AuditID, &when)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return authn.RecoveryResult{}, storeError("read recovery status", err)
	}
	result.Result = "applied"
	result.PasswordChangedAt = when.UTC().Format(authn.RecoveryTimeLayout)
	return result, nil
}
func (s *Store) ApplyRecovery(ctx context.Context, r authn.RecoveryRecord) (result authn.RecoveryResult, err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, storeError("begin recovery", err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		rollbackErr := tx.Rollback(rollbackCtx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = errors.Join(err, storeError("rollback recovery", rollbackErr))
		}
	}()
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='5s'`); err != nil {
		return result, storeError("set recovery lock budget", err)
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM radishnexus.user_accounts WHERE user_id=$1 FOR UPDATE`, r.Candidate.UserID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, authn.ErrRecoveryIneligible
		}
		return result, storeError("lock recovery account", err)
	}
	var c authn.RecoveryCandidate
	c.UserID = r.Candidate.UserID
	c.AccountStatus = status
	err = tx.QueryRow(ctx, `SELECT coalesce(email,''),status,password_hash,password_changed_at FROM radishnexus.local_credentials WHERE user_id=$1 FOR UPDATE`, c.UserID).Scan(&c.Email, &c.CredentialStatus, &c.PasswordHash, &c.PasswordChangedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, authn.ErrRecoveryIneligible
	}
	if err != nil {
		return result, storeError("lock recovery credential", err)
	}
	var used bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM radishnexus.identity_audit WHERE operation_id=$1::uuid)`, r.OperationID).Scan(&used); err != nil {
		return result, storeError("recheck recovery operation", err)
	}
	if used {
		return result, authn.ErrRecoveryRecorded
	}
	if !c.Eligible() {
		return result, authn.ErrRecoveryIneligible
	}
	if c.Email != r.Candidate.Email || c.PasswordHash != r.Candidate.PasswordHash || !c.PasswordChangedAt.Equal(r.Candidate.PasswordChangedAt) {
		return result, authn.ErrRecoveryConflict
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return result, storeError("read recovery clock", err)
	}
	if !now.After(c.PasswordChangedAt) {
		return result, authn.ErrRecoveryClock
	}
	if _, err = tx.Exec(ctx, `UPDATE radishnexus.local_credentials SET password_hash=$2,password_changed_at=$3,failed_login_count=0,locked_until=NULL WHERE user_id=$1`, c.UserID, r.PasswordHash, now); err != nil {
		return result, storeError("update recovered credential", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE radishnexus.user_sessions SET revoked_at=greatest($2::timestamptz,created_at) WHERE user_id=$1 AND revoked_at IS NULL`, c.UserID, now); err != nil {
		return result, storeError("revoke recovered sessions", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO radishnexus.identity_audit(id,action,actor_kind,operator_ref,target_user_id,operation_id,reason_code,previous_password_changed_at,applied_password_changed_at,occurred_at) VALUES($1,'local.password.recovered','operator',$2,$3,$4::uuid,$5,$6,$7,$7)`, r.AuditID, r.OperatorRef, c.UserID, r.OperationID, r.ReasonCode, c.PasswordChangedAt, now); err != nil {
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) && pgerr.Code == "23505" && pgerr.ConstraintName == "identity_audit_operation_id_key" {
			return result, authn.ErrRecoveryRecorded
		}
		return result, storeError("record password recovery", err)
	}
	if err = ctx.Err(); err != nil {
		return result, storeError("recovery canceled before commit", err)
	}
	if err = tx.Commit(ctx); err != nil {
		// An ordinary PostgreSQL ERROR rejects COMMIT. A FATAL connection
		// termination may arrive without a commit outcome, so remains uncertain.
		var pgerr *pgconn.PgError
		if errors.Is(err, pgx.ErrTxCommitRollback) || (errors.As(err, &pgerr) && pgerr.Severity == "ERROR") {
			return result, storeError("recovery commit rejected", err)
		}
		return result, errors.Join(authn.ErrRecoveryUnconfirmed, storeError("recovery commit acknowledgement", err))
	}
	return authn.RecoveryResult{Result: "applied", UserID: c.UserID, OperationID: r.OperationID, AuditID: r.AuditID, PasswordChangedAt: now.UTC().Format(authn.RecoveryTimeLayout)}, nil
}
