package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func (store *Store) AuthorizeChannelRead(
	ctx context.Context,
	principal authz.Principal,
	channelID string,
) (err error) {
	if err := principal.ValidateUser(); err != nil {
		return err
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin authorize Channel read transaction: %w", err)
	}
	defer rollback(ctx, tx, &err)
	if _, err := readableChannel(ctx, tx, principal, channelID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit authorize Channel read: %w", err)
	}
	return nil
}

func (store *Store) GetChannelMessage(
	ctx context.Context,
	principal authz.Principal,
	channelID string,
	messageID string,
) (message goldenpath.MessageProjection, err error) {
	if err := principal.ValidateUser(); err != nil {
		return message, err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return message, fmt.Errorf("begin get Channel Message transaction: %w", err)
	}
	defer rollback(ctx, tx, &err)
	facts, err := readableMessage(ctx, tx, principal, messageID)
	if err != nil {
		return message, err
	}
	if facts.channelID != channelID {
		return message, authz.ErrNotFound
	}
	var threadID sql.NullString
	err = tx.QueryRow(ctx, `
		SELECT id, channel_id, thread_id, author_id, body, created_at
		FROM radishnexus.messages
		WHERE workspace_id = $1 AND channel_id = $2 AND id = $3
		FOR SHARE
	`, principal.WorkspaceID, channelID, messageID).Scan(
		&message.ID,
		&message.ChannelID,
		&threadID,
		&message.AuthorID,
		&message.Body,
		&message.CreatedAt,
	)
	if err != nil {
		return goldenpath.MessageProjection{}, mapDatabaseError("load Channel Message", err)
	}
	if threadID.Valid {
		value := threadID.String
		message.ThreadID = &value
	}
	if err := tx.Commit(ctx); err != nil {
		return goldenpath.MessageProjection{}, fmt.Errorf("commit get Channel Message: %w", err)
	}
	return message, nil
}
