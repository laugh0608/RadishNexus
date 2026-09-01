INSERT INTO radishnexus.entity_types (type_name, id_prefix) VALUES
    ('channel', 'chn_'),
    ('message', 'msg_');

CREATE TABLE radishnexus.channels (
    id text PRIMARY KEY,
    workspace_id text NOT NULL,
    governing_project_id text NOT NULL,
    name text NOT NULL,
    visibility text NOT NULL,
    status text NOT NULL,
    created_by_kind text NOT NULL,
    created_by_id text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT channels_workspace_id_unique UNIQUE (workspace_id, id),
    CONSTRAINT channels_project_scope_unique UNIQUE (
        workspace_id, id, governing_project_id
    ),
    FOREIGN KEY (workspace_id, governing_project_id)
        REFERENCES radishnexus.projects (workspace_id, id),
    CONSTRAINT channels_id_format CHECK (
        radishnexus.valid_entity_id('channel', id)
    ),
    CONSTRAINT channels_name_nonempty CHECK (btrim(name) <> ''),
    CONSTRAINT channels_visibility CHECK (visibility IN ('project', 'restricted')),
    CONSTRAINT channels_status CHECK (status IN ('active', 'archived')),
    CONSTRAINT channels_actor CHECK (
        created_by_kind IN ('user', 'system', 'plugin', 'import')
        AND (created_by_kind = 'system' OR created_by_id IS NOT NULL)
    )
);

CREATE TRIGGER channels_scope_immutable
BEFORE UPDATE ON radishnexus.channels
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_scoped_identity_change();

CREATE TABLE radishnexus.channel_memberships (
    workspace_id text NOT NULL,
    channel_id text NOT NULL,
    user_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (workspace_id, channel_id, user_id),
    FOREIGN KEY (workspace_id, channel_id)
        REFERENCES radishnexus.channels (workspace_id, id),
    FOREIGN KEY (workspace_id, user_id)
        REFERENCES radishnexus.workspace_memberships (workspace_id, user_id)
);

ALTER TABLE radishnexus.threads
ADD COLUMN origin_channel_id text;

ALTER TABLE radishnexus.threads
ADD CONSTRAINT threads_origin_channel_scope
FOREIGN KEY (workspace_id, origin_channel_id, governing_project_id)
REFERENCES radishnexus.channels (workspace_id, id, governing_project_id);

ALTER TABLE radishnexus.threads
ADD CONSTRAINT threads_origin_reference_unique
UNIQUE (workspace_id, id, origin_channel_id);

CREATE FUNCTION radishnexus.prevent_thread_origin_change()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    IF NEW.origin_channel_id IS DISTINCT FROM OLD.origin_channel_id THEN
        RAISE EXCEPTION 'Thread origin Channel cannot change for %', OLD.id
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER threads_origin_immutable
BEFORE UPDATE ON radishnexus.threads
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_thread_origin_change();

CREATE TABLE radishnexus.messages (
    id text PRIMARY KEY,
    workspace_id text NOT NULL,
    channel_id text NOT NULL,
    thread_id text,
    author_id text NOT NULL,
    body text NOT NULL,
    client_operation_id text NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT messages_workspace_id_unique UNIQUE (workspace_id, id),
    CONSTRAINT messages_client_operation_unique UNIQUE (
        workspace_id, channel_id, author_id, client_operation_id
    ),
    FOREIGN KEY (workspace_id, channel_id)
        REFERENCES radishnexus.channels (workspace_id, id),
    FOREIGN KEY (workspace_id, thread_id, channel_id)
        REFERENCES radishnexus.threads (workspace_id, id, origin_channel_id),
    FOREIGN KEY (workspace_id, author_id)
        REFERENCES radishnexus.workspace_memberships (workspace_id, user_id),
    CONSTRAINT messages_id_format CHECK (
        radishnexus.valid_entity_id('message', id)
    ),
    CONSTRAINT messages_body CHECK (
        body ~ '[^[:space:]]' AND octet_length(body) <= 16384
    ),
    CONSTRAINT messages_client_operation CHECK (
        octet_length(client_operation_id) BETWEEN 1 AND 128
        AND octet_length(client_operation_id) = char_length(client_operation_id)
        AND client_operation_id ~ '^[!-~]+$'
    )
);

CREATE FUNCTION radishnexus.prevent_message_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    RAISE EXCEPTION 'Message facts are immutable'
        USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER messages_immutable
BEFORE UPDATE OR DELETE ON radishnexus.messages
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_message_mutation();

INSERT INTO radishnexus.relation_types (from_type, relation_type, to_type)
VALUES ('thread', 'started-from', 'message');

CREATE UNIQUE INDEX entity_links_thread_started_from_message
ON radishnexus.entity_links (workspace_id, to_id)
WHERE from_type = 'thread'
  AND relation_type = 'started-from'
  AND to_type = 'message';

CREATE FUNCTION radishnexus.prevent_thread_source_relation_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    RAISE EXCEPTION 'Thread started-from relation is immutable'
        USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER entity_links_thread_source_immutable
BEFORE UPDATE OR DELETE ON radishnexus.entity_links
FOR EACH ROW
WHEN (
    OLD.from_type = 'thread'
    AND OLD.relation_type = 'started-from'
    AND OLD.to_type = 'message'
)
EXECUTE FUNCTION radishnexus.prevent_thread_source_relation_mutation();

CREATE OR REPLACE FUNCTION radishnexus.entity_workspace(entity_type text, entity_id text)
RETURNS text
LANGUAGE plpgsql
STABLE
SET search_path = radishnexus, pg_temp
AS $$
DECLARE
    resolved_workspace text;
BEGIN
    CASE entity_type
        WHEN 'project' THEN
            SELECT workspace_id INTO resolved_workspace FROM projects WHERE id = entity_id;
        WHEN 'component' THEN
            SELECT workspace_id INTO resolved_workspace FROM components WHERE id = entity_id;
        WHEN 'environment' THEN
            SELECT workspace_id INTO resolved_workspace FROM environments WHERE id = entity_id;
        WHEN 'channel' THEN
            SELECT workspace_id INTO resolved_workspace FROM channels WHERE id = entity_id;
        WHEN 'message' THEN
            SELECT workspace_id INTO resolved_workspace FROM messages WHERE id = entity_id;
        WHEN 'thread' THEN
            SELECT workspace_id INTO resolved_workspace FROM threads WHERE id = entity_id;
        WHEN 'decision' THEN
            SELECT workspace_id INTO resolved_workspace FROM decisions WHERE id = entity_id;
        WHEN 'ticket' THEN
            SELECT workspace_id INTO resolved_workspace FROM tickets WHERE id = entity_id;
        WHEN 'ci-run' THEN
            SELECT workspace_id INTO resolved_workspace FROM ci_runs WHERE id = entity_id;
        WHEN 'deployment' THEN
            SELECT workspace_id INTO resolved_workspace FROM deployments WHERE id = entity_id;
        ELSE
            RETURN NULL;
    END CASE;
    RETURN resolved_workspace;
END;
$$;

CREATE FUNCTION radishnexus.validate_thread_source_relation(
    checked_workspace_id text,
    checked_thread_id text
)
RETURNS void
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
DECLARE
    thread_channel_id text;
    matching_sources integer;
    all_sources integer;
BEGIN
    SELECT origin_channel_id INTO thread_channel_id
    FROM threads
    WHERE workspace_id = checked_workspace_id AND id = checked_thread_id;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT count(*) INTO all_sources
    FROM entity_links
    WHERE workspace_id = checked_workspace_id
      AND from_type = 'thread'
      AND from_id = checked_thread_id
      AND relation_type = 'started-from'
      AND to_type = 'message';

    IF thread_channel_id IS NULL THEN
        IF all_sources <> 0 THEN
            RAISE EXCEPTION 'non-messaging Thread cannot have a started-from Message'
                USING ERRCODE = '23514';
        END IF;
        RETURN;
    END IF;

    SELECT count(*) INTO matching_sources
    FROM entity_links AS source_link
    JOIN messages AS source_message
      ON source_message.workspace_id = source_link.workspace_id
     AND source_message.id = source_link.to_id
    WHERE source_link.workspace_id = checked_workspace_id
      AND source_link.from_type = 'thread'
      AND source_link.from_id = checked_thread_id
      AND source_link.relation_type = 'started-from'
      AND source_link.to_type = 'message'
      AND source_link.state = 'active'
      AND source_message.channel_id = thread_channel_id;

    IF all_sources <> 1 OR matching_sources <> 1 THEN
        RAISE EXCEPTION 'messaging Thread requires exactly one source Message in its origin Channel'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE FUNCTION radishnexus.validate_thread_source_from_row()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    PERFORM validate_thread_source_relation(NEW.workspace_id, NEW.id);
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER threads_require_source_message
AFTER INSERT OR UPDATE ON radishnexus.threads
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_thread_source_from_row();

CREATE FUNCTION radishnexus.validate_thread_source_from_link()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    IF TG_OP <> 'DELETE'
       AND NEW.from_type = 'thread'
       AND NEW.relation_type = 'started-from'
       AND NEW.to_type = 'message' THEN
        PERFORM validate_thread_source_relation(NEW.workspace_id, NEW.from_id);
    END IF;
    IF TG_OP <> 'INSERT'
       AND OLD.from_type = 'thread'
       AND OLD.relation_type = 'started-from'
       AND OLD.to_type = 'message' THEN
        PERFORM validate_thread_source_relation(OLD.workspace_id, OLD.from_id);
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER entity_links_keep_thread_source
AFTER INSERT OR UPDATE OR DELETE ON radishnexus.entity_links
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_thread_source_from_link();

CREATE INDEX channel_memberships_user
ON radishnexus.channel_memberships (workspace_id, user_id, channel_id);

CREATE INDEX messages_channel_chronology
ON radishnexus.messages (workspace_id, channel_id, created_at, id);

CREATE INDEX messages_thread_chronology
ON radishnexus.messages (workspace_id, thread_id, created_at, id)
WHERE thread_id IS NOT NULL;

---- create above / drop below ----

DROP INDEX radishnexus.messages_thread_chronology;
DROP INDEX radishnexus.messages_channel_chronology;
DROP INDEX radishnexus.channel_memberships_user;
DROP TRIGGER entity_links_keep_thread_source ON radishnexus.entity_links;
DROP FUNCTION radishnexus.validate_thread_source_from_link();
DROP TRIGGER threads_require_source_message ON radishnexus.threads;
DROP FUNCTION radishnexus.validate_thread_source_from_row();
DROP FUNCTION radishnexus.validate_thread_source_relation(text, text);
DROP TRIGGER entity_links_thread_source_immutable ON radishnexus.entity_links;
DROP FUNCTION radishnexus.prevent_thread_source_relation_mutation();
DROP INDEX radishnexus.entity_links_thread_started_from_message;
DELETE FROM radishnexus.relation_types
WHERE from_type = 'thread'
  AND relation_type = 'started-from'
  AND to_type = 'message';
DROP TRIGGER messages_immutable ON radishnexus.messages;
DROP FUNCTION radishnexus.prevent_message_mutation();
DROP TABLE radishnexus.messages;
DROP TRIGGER threads_origin_immutable ON radishnexus.threads;
DROP FUNCTION radishnexus.prevent_thread_origin_change();
ALTER TABLE radishnexus.threads DROP CONSTRAINT threads_origin_reference_unique;
ALTER TABLE radishnexus.threads DROP CONSTRAINT threads_origin_channel_scope;
ALTER TABLE radishnexus.threads DROP COLUMN origin_channel_id;
DROP TABLE radishnexus.channel_memberships;
DROP TRIGGER channels_scope_immutable ON radishnexus.channels;
DROP TABLE radishnexus.channels;
DELETE FROM radishnexus.entity_types WHERE type_name IN ('channel', 'message');
