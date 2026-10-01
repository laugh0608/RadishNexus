CREATE TABLE radishnexus.workspace_configuration_audit (
    id text PRIMARY KEY CHECK (id LIKE 'cfa_%'),
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces(id),
    actor_id text NOT NULL REFERENCES radishnexus.users(id),
    command_kind text NOT NULL CHECK (command_kind IN (
        'team.create', 'project.create', 'channel.create',
        'project.member.set', 'project.member.remove',
        'channel.member.add', 'channel.member.remove'
    )),
    scope_id text NOT NULL,
    subject_id text NOT NULL,
    result_id text NOT NULL,
    before_state text NOT NULL CHECK (before_state IN ('', 'viewer', 'contributor', 'decider', 'admin', 'member')),
    after_state text NOT NULL CHECK (after_state IN ('', 'viewer', 'contributor', 'decider', 'admin', 'member')),
    changed boolean NOT NULL,
    granted_user_ids text[] NOT NULL DEFAULT '{}',
    removed_channel_ids text[] NOT NULL DEFAULT '{}',
    removed_thread_ids text[] NOT NULL DEFAULT '{}',
    request_id text NOT NULL,
    occurred_at timestamptz NOT NULL,
    UNIQUE (workspace_id, id),
    CHECK (
        (command_kind IN ('team.create', 'project.create') AND scope_id = workspace_id AND subject_id = '')
        OR (command_kind = 'channel.create' AND scope_id LIKE 'prj_%' AND subject_id = '')
        OR (command_kind IN ('project.member.set', 'project.member.remove') AND scope_id LIKE 'prj_%' AND subject_id LIKE 'usr_%')
        OR (command_kind IN ('channel.member.add', 'channel.member.remove') AND scope_id LIKE 'chn_%' AND subject_id LIKE 'usr_%')
    ),
    CHECK (
        (command_kind = 'team.create' AND result_id LIKE 'tem_%')
        OR (command_kind = 'project.create' AND result_id LIKE 'prj_%')
        OR (command_kind = 'channel.create' AND result_id LIKE 'chn_%')
        OR (command_kind IN ('project.member.set', 'project.member.remove', 'channel.member.add', 'channel.member.remove') AND result_id = subject_id)
    )
);

CREATE TABLE radishnexus.workspace_configuration_receipts (
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces(id),
    actor_id text NOT NULL REFERENCES radishnexus.users(id),
    command_kind text NOT NULL,
    scope_id text NOT NULL,
    subject_id text NOT NULL,
    client_operation_id text NOT NULL CHECK (
        octet_length(client_operation_id) BETWEEN 1 AND 128
        AND octet_length(client_operation_id) = char_length(client_operation_id)
        AND client_operation_id ~ '^[!-~]+$'
    ),
    payload_sha256 text NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    audit_id text NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, actor_id, command_kind, scope_id, subject_id, client_operation_id),
    FOREIGN KEY (workspace_id, audit_id) REFERENCES radishnexus.workspace_configuration_audit(workspace_id,id),
    CHECK (
        (command_kind IN ('team.create', 'project.create') AND scope_id = workspace_id AND subject_id = '')
        OR (command_kind = 'channel.create' AND scope_id LIKE 'prj_%' AND subject_id = '')
        OR (command_kind IN ('project.member.set', 'project.member.remove') AND scope_id LIKE 'prj_%' AND subject_id LIKE 'usr_%')
        OR (command_kind IN ('channel.member.add', 'channel.member.remove') AND scope_id LIKE 'chn_%' AND subject_id LIKE 'usr_%')
    )
);

CREATE FUNCTION radishnexus.prevent_configuration_evidence_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Configuration audit and receipts are immutable' USING ERRCODE = '23514';
END;
$$;
CREATE TRIGGER configuration_audit_immutable BEFORE UPDATE OR DELETE
ON radishnexus.workspace_configuration_audit FOR EACH ROW
EXECUTE FUNCTION radishnexus.prevent_configuration_evidence_mutation();
CREATE TRIGGER configuration_receipts_immutable BEFORE UPDATE OR DELETE
ON radishnexus.workspace_configuration_receipts FOR EACH ROW
EXECUTE FUNCTION radishnexus.prevent_configuration_evidence_mutation();

---- create above / drop below ----
-- Forward-only; restore the pre-upgrade backup to an empty target to roll back.
