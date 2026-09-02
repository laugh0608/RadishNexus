CREATE TABLE radishnexus.collaboration_command_receipts (
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces (id),
    actor_id text NOT NULL REFERENCES radishnexus.users (id),
    command_kind text NOT NULL,
    target_type text NOT NULL REFERENCES radishnexus.entity_types (type_name),
    target_id text NOT NULL,
    client_operation_id text NOT NULL,
    payload_sha256 text NOT NULL,
    result_type text NOT NULL REFERENCES radishnexus.entity_types (type_name),
    result_id text NOT NULL,
    event_id text NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (
        workspace_id,
        actor_id,
        command_kind,
        target_type,
        target_id,
        client_operation_id
    ),
    FOREIGN KEY (workspace_id, event_id)
        REFERENCES radishnexus.domain_events (workspace_id, event_id)
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT collaboration_command_receipts_kind CHECK (
        command_kind IN ('decision.propose', 'decision.accept', 'ticket.create')
    ),
    CONSTRAINT collaboration_command_receipts_target CHECK (
        (command_kind = 'decision.propose' AND target_type = 'thread')
        OR (command_kind IN ('decision.accept', 'ticket.create') AND target_type = 'decision')
    ),
    CONSTRAINT collaboration_command_receipts_result CHECK (
        (command_kind IN ('decision.propose', 'decision.accept') AND result_type = 'decision')
        OR (command_kind = 'ticket.create' AND result_type = 'ticket')
    ),
    CONSTRAINT collaboration_command_receipts_entity_ids CHECK (
        radishnexus.valid_entity_id(target_type, target_id)
        AND radishnexus.valid_entity_id(result_type, result_id)
        AND event_id LIKE 'evt_%'
    ),
    CONSTRAINT collaboration_command_receipts_operation_id CHECK (
        octet_length(client_operation_id) BETWEEN 1 AND 128
        AND octet_length(client_operation_id) = char_length(client_operation_id)
        AND client_operation_id ~ '^[!-~]+$'
    ),
    CONSTRAINT collaboration_command_receipts_digest CHECK (
        payload_sha256 ~ '^[0-9a-f]{64}$'
    )
);

CREATE FUNCTION radishnexus.prevent_collaboration_command_receipt_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    RAISE EXCEPTION 'Collaboration command receipts are immutable'
        USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER collaboration_command_receipts_immutable
BEFORE UPDATE OR DELETE ON radishnexus.collaboration_command_receipts
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_collaboration_command_receipt_mutation();
