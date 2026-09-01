package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

func (store *Store) CreateMessage(
	ctx context.Context,
	command goldenpath.CreateMessageCommand,
) (result goldenpath.CreateMessageResult, err error) {
	if err := command.Principal.ValidateUser(); err != nil {
		return result, err
	}

	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin create Message transaction: %w", err)
	}
	defer rollback(ctx, tx, &err)

	channel, err := readableChannel(ctx, tx, command.Principal, command.ChannelID)
	if err != nil {
		return result, err
	}
	if !channel.project.active || channel.status != "active" {
		return result, fmt.Errorf("%w: archived Project or Channel does not accept Messages", authz.ErrConflict)
	}
	if err := authz.RequireContribute(channel.project.role); err != nil {
		return result, err
	}

	existing, found, err := loadMessageByOperation(ctx, tx, command)
	if err != nil {
		return result, err
	}
	if found {
		if err := requireExistingMessageAccess(ctx, tx, command.Principal, existing); err != nil {
			return result, err
		}
		if existing.Body != command.Body {
			return result, fmt.Errorf("%w: client operation payload changed", authz.ErrConflict)
		}
		if err := tx.Commit(ctx); err != nil {
			return result, fmt.Errorf("commit duplicate Message lookup: %w", err)
		}
		return goldenpath.CreateMessageResult{Message: existing}, nil
	}

	if command.ThreadID != "" {
		if err := requireReadableThreadInChannel(
			ctx,
			tx,
			command.Principal,
			command.ThreadID,
			command.ChannelID,
		); err != nil {
			return result, err
		}
	}

	message, err := scanMessageRow(tx.QueryRow(ctx, `
		INSERT INTO radishnexus.messages (
			id, workspace_id, channel_id, thread_id, author_id,
			body, client_operation_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (workspace_id, channel_id, author_id, client_operation_id)
		DO NOTHING
		RETURNING id, workspace_id, channel_id, thread_id, author_id,
			body, client_operation_id, created_at
	`, command.MessageID, command.Principal.WorkspaceID, command.ChannelID,
		nullable(command.ThreadID), command.Principal.ID, command.Body,
		command.ClientOperationID, command.OccurredAt))
	if errors.Is(err, pgx.ErrNoRows) {
		existing, found, loadErr := loadMessageByOperation(ctx, tx, command)
		if loadErr != nil {
			return result, loadErr
		}
		if !found {
			return result, errors.New("concurrent Message idempotency winner is missing")
		}
		if err := requireExistingMessageAccess(ctx, tx, command.Principal, existing); err != nil {
			return result, err
		}
		if existing.Body != command.Body {
			return result, fmt.Errorf("%w: client operation payload changed", authz.ErrConflict)
		}
		if err := tx.Commit(ctx); err != nil {
			return result, fmt.Errorf("commit concurrent duplicate Message lookup: %w", err)
		}
		return goldenpath.CreateMessageResult{Message: existing}, nil
	}
	if err != nil {
		return result, mapDatabaseError("insert Message", err)
	}

	payload := map[string]any{
		"channel": map[string]string{"type": "channel", "id": command.ChannelID},
	}
	if command.ThreadID != "" {
		payload["thread"] = map[string]string{"type": "thread", "id": command.ThreadID}
	}
	if err := insertEvent(ctx, tx, eventRecord{
		ID:            command.EventID,
		Type:          "message.created",
		WorkspaceID:   command.Principal.WorkspaceID,
		ActorKind:     "user",
		ActorID:       command.Principal.ID,
		SourceKind:    command.SourceKind,
		SourceID:      command.SourceID,
		PrimaryType:   "message",
		PrimaryID:     command.MessageID,
		ProjectID:     channel.projectID,
		CorrelationID: command.CorrelationID,
		CausationID:   command.CausationID,
		OccurredAt:    command.OccurredAt,
		Payload:       payload,
	}); err != nil {
		return result, err
	}
	if err := insertOutboxFor(ctx, tx, command.EventID, "realtime-dispatcher"); err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, mapDatabaseError("commit Message", err)
	}
	return goldenpath.CreateMessageResult{Message: message, Created: true}, nil
}

func (store *Store) StartThreadFromMessage(
	ctx context.Context,
	command goldenpath.StartThreadFromMessageCommand,
) (thread goldenpath.Thread, err error) {
	if err := command.Principal.ValidateUser(); err != nil {
		return thread, err
	}

	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return thread, fmt.Errorf("begin start Thread transaction: %w", err)
	}
	defer rollback(ctx, tx, &err)

	message, err := readableMessage(ctx, tx, command.Principal, command.MessageID)
	if err != nil {
		return thread, err
	}
	channel, err := readableChannel(ctx, tx, command.Principal, message.channelID)
	if err != nil {
		return thread, err
	}
	if !channel.project.active || channel.status != "active" {
		return thread, fmt.Errorf("%w: archived Project or Channel does not accept Threads", authz.ErrConflict)
	}
	if err := authz.RequireContribute(channel.project.role); err != nil {
		return thread, err
	}
	if channel.visibility == "restricted" && command.Visibility != "restricted" {
		return thread, fmt.Errorf("%w: restricted Channel cannot create a project-visible Thread", authz.ErrConflict)
	}

	var sourceUsed bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM radishnexus.entity_links
			WHERE workspace_id = $1
			  AND from_type = 'thread'
			  AND relation_type = 'started-from'
			  AND to_type = 'message'
			  AND to_id = $2
		)
	`, command.Principal.WorkspaceID, command.MessageID).Scan(&sourceUsed); err != nil {
		return thread, fmt.Errorf("check Message Thread source: %w", err)
	}
	if sourceUsed {
		return thread, fmt.Errorf("%w: Message already started a Thread", authz.ErrConflict)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO radishnexus.threads (
			id, workspace_id, governing_project_id, origin_channel_id,
			title, visibility, created_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
	`, command.ThreadID, command.Principal.WorkspaceID, channel.projectID,
		message.channelID, command.Title, command.Visibility,
		command.Principal.ID, command.OccurredAt)
	if err != nil {
		return thread, mapDatabaseError("insert messaging Thread", err)
	}
	if command.Visibility == "restricted" {
		_, err = tx.Exec(ctx, `
			INSERT INTO radishnexus.thread_memberships (
				workspace_id, thread_id, user_id, created_at
			) VALUES ($1, $2, $3, $4)
		`, command.Principal.WorkspaceID, command.ThreadID,
			command.Principal.ID, command.OccurredAt)
		if err != nil {
			return thread, mapDatabaseError("insert Thread creator membership", err)
		}
	}

	if err := insertEvent(ctx, tx, eventRecord{
		ID:            command.EventID,
		Type:          "thread.started",
		WorkspaceID:   command.Principal.WorkspaceID,
		ActorKind:     "user",
		ActorID:       command.Principal.ID,
		SourceKind:    command.SourceKind,
		SourceID:      command.SourceID,
		PrimaryType:   "thread",
		PrimaryID:     command.ThreadID,
		ProjectID:     channel.projectID,
		CorrelationID: command.CorrelationID,
		CausationID:   command.CausationID,
		OccurredAt:    command.OccurredAt,
		Payload: map[string]any{
			"channel": map[string]string{"type": "channel", "id": message.channelID},
			"message": map[string]string{"type": "message", "id": command.MessageID},
		},
	}); err != nil {
		return thread, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO radishnexus.entity_links (
			id, workspace_id, from_type, from_id, relation_type, to_type, to_id,
			assertion, origin, created_by_kind, created_by_id,
			created_at, updated_at, source_event_id
		) VALUES ($1, $2, 'thread', $3, 'started-from', 'message', $4,
			'asserted', 'user', 'user', $5, $6, $6, $7)
	`, command.LinkID, command.Principal.WorkspaceID, command.ThreadID,
		command.MessageID, command.Principal.ID, command.OccurredAt, command.EventID)
	if err != nil {
		return thread, mapDatabaseError("insert Thread source link", err)
	}
	if err := insertOutboxFor(ctx, tx, command.EventID, "realtime-dispatcher"); err != nil {
		return thread, err
	}
	if err := tx.Commit(ctx); err != nil {
		return thread, mapDatabaseError("commit messaging Thread", err)
	}

	originChannelID := message.channelID
	return goldenpath.Thread{
		ID:                 command.ThreadID,
		WorkspaceID:        command.Principal.WorkspaceID,
		GoverningProjectID: channel.projectID,
		OriginChannelID:    &originChannelID,
		Title:              command.Title,
		Visibility:         command.Visibility,
		CreatedBy:          command.Principal.ID,
		CreatedAt:          command.OccurredAt,
		UpdatedAt:          command.OccurredAt,
	}, nil
}

type messageRow interface {
	Scan(dest ...any) error
}

func scanMessageRow(row messageRow) (goldenpath.Message, error) {
	var message goldenpath.Message
	var threadID sql.NullString
	if err := row.Scan(
		&message.ID,
		&message.WorkspaceID,
		&message.ChannelID,
		&threadID,
		&message.AuthorID,
		&message.Body,
		&message.ClientOperationID,
		&message.CreatedAt,
	); err != nil {
		return goldenpath.Message{}, err
	}
	if threadID.Valid {
		value := threadID.String
		message.ThreadID = &value
	}
	return message, nil
}

func loadMessageByOperation(
	ctx context.Context,
	tx pgx.Tx,
	command goldenpath.CreateMessageCommand,
) (goldenpath.Message, bool, error) {
	message, err := scanMessageRow(tx.QueryRow(ctx, `
		SELECT id, workspace_id, channel_id, thread_id, author_id,
			body, client_operation_id, created_at
		FROM radishnexus.messages
		WHERE workspace_id = $1
		  AND channel_id = $2
		  AND author_id = $3
		  AND client_operation_id = $4
		FOR SHARE
	`, command.Principal.WorkspaceID, command.ChannelID,
		command.Principal.ID, command.ClientOperationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return goldenpath.Message{}, false, nil
	}
	if err != nil {
		return goldenpath.Message{}, false, fmt.Errorf("load Message idempotency record: %w", err)
	}
	return message, true, nil
}

func requireExistingMessageAccess(
	ctx context.Context,
	tx pgx.Tx,
	principal authz.Principal,
	message goldenpath.Message,
) error {
	if message.ThreadID == nil {
		return nil
	}
	return requireReadableThreadInChannel(
		ctx,
		tx,
		principal,
		*message.ThreadID,
		message.ChannelID,
	)
}
