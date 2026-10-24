package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/workspaceauth"
)

func (s *Store) beginAdmission(ctx context.Context, p authz.Principal, options pgx.TxOptions) (pgx.Tx, error) {
	return s.beginWorkspaceAdmission(ctx, p, options, false)
}

func (s *Store) beginWorkspaceAdmission(ctx context.Context, p authz.Principal, options pgx.TxOptions, exclusive bool) (pgx.Tx, error) {
	tx, err := s.pool.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	if err = workspaceauth.Lock(ctx, tx, p.WorkspaceID, exclusive); err == nil {
		err = workspaceauth.Check(ctx, tx, p)
	}
	if err != nil {
		return nil, errors.Join(err, workspaceauth.Rollback(ctx, tx))
	}
	return tx, nil
}
