CREATE SCHEMA radishnexus;

CREATE TABLE radishnexus.entity_types (
    type_name text PRIMARY KEY,
    id_prefix text NOT NULL,
    CONSTRAINT entity_types_name_format CHECK (
        type_name ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$'
    ),
    CONSTRAINT entity_types_prefix_format CHECK (
        id_prefix ~ '^[a-z][a-z0-9]*_$'
    )
);

INSERT INTO radishnexus.entity_types (type_name, id_prefix) VALUES
    ('project', 'prj_'),
    ('initiative', 'ini_'),
    ('component', 'cmp_'),
    ('decision', 'dec_'),
    ('environment', 'env_'),
    ('entity-link', 'lnk_'),
    ('thread', 'thr_'),
    ('ticket', 'tkt_');

CREATE FUNCTION radishnexus.valid_entity_id(entity_type text, entity_id text)
RETURNS boolean
LANGUAGE sql
STABLE
SET search_path = radishnexus, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM entity_types
        WHERE type_name = entity_type
          AND entity_id <> ''
          AND entity_id !~ '[/\?#[:space:]]'
          AND octet_length(entity_id) = char_length(entity_id)
          AND left(entity_id, length(id_prefix)) = id_prefix
    );
$$;

CREATE TABLE radishnexus.users (
    id text PRIMARY KEY,
    display_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT users_id_format CHECK (id LIKE 'usr_%')
);

CREATE TABLE radishnexus.workspaces (
    id text PRIMARY KEY,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT workspaces_id_format CHECK (id LIKE 'wrk_%')
);

CREATE TABLE radishnexus.workspace_memberships (
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces (id),
    user_id text NOT NULL REFERENCES radishnexus.users (id),
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (workspace_id, user_id),
    CONSTRAINT workspace_memberships_status CHECK (status IN ('active', 'suspended'))
);

CREATE TABLE radishnexus.teams (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces (id),
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (workspace_id, id),
    CONSTRAINT teams_id_format CHECK (id LIKE 'tem_%')
);

CREATE TABLE radishnexus.projects (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces (id),
    key text NOT NULL,
    name text NOT NULL,
    summary text,
    owner_team_id text NOT NULL,
    visibility text NOT NULL,
    status text NOT NULL,
    created_by_kind text NOT NULL,
    created_by_id text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, key),
    FOREIGN KEY (workspace_id, owner_team_id)
        REFERENCES radishnexus.teams (workspace_id, id),
    CONSTRAINT projects_id_format CHECK (radishnexus.valid_entity_id('project', id)),
    CONSTRAINT projects_visibility CHECK (visibility IN ('workspace', 'restricted')),
    CONSTRAINT projects_status CHECK (status IN ('active', 'archived')),
    CONSTRAINT projects_actor CHECK (
        created_by_kind IN ('user', 'system', 'plugin', 'import')
        AND (created_by_kind = 'system' OR created_by_id IS NOT NULL)
    )
);

CREATE TABLE radishnexus.project_memberships (
    workspace_id text NOT NULL,
    project_id text NOT NULL,
    user_id text NOT NULL,
    role text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (workspace_id, project_id, user_id),
    FOREIGN KEY (workspace_id, project_id)
        REFERENCES radishnexus.projects (workspace_id, id),
    FOREIGN KEY (workspace_id, user_id)
        REFERENCES radishnexus.workspace_memberships (workspace_id, user_id),
    CONSTRAINT project_memberships_role CHECK (
        role IN ('viewer', 'contributor', 'decider', 'admin')
    )
);

CREATE TABLE radishnexus.threads (
    id text PRIMARY KEY,
    workspace_id text NOT NULL,
    governing_project_id text NOT NULL,
    title text NOT NULL,
    visibility text NOT NULL,
    created_by text NOT NULL REFERENCES radishnexus.users (id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, governing_project_id)
        REFERENCES radishnexus.projects (workspace_id, id),
    CONSTRAINT threads_id_format CHECK (radishnexus.valid_entity_id('thread', id)),
    CONSTRAINT threads_visibility CHECK (visibility IN ('project', 'restricted'))
);

CREATE TABLE radishnexus.thread_memberships (
    workspace_id text NOT NULL,
    thread_id text NOT NULL,
    user_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (workspace_id, thread_id, user_id),
    FOREIGN KEY (workspace_id, thread_id)
        REFERENCES radishnexus.threads (workspace_id, id),
    FOREIGN KEY (workspace_id, user_id)
        REFERENCES radishnexus.workspace_memberships (workspace_id, user_id)
);

CREATE TABLE radishnexus.decisions (
    id text PRIMARY KEY,
    workspace_id text NOT NULL,
    governing_project_id text NOT NULL,
    question text NOT NULL,
    outcome text,
    status text NOT NULL,
    proposer_id text NOT NULL REFERENCES radishnexus.users (id),
    decider_ids text[] NOT NULL DEFAULT '{}',
    decided_at timestamptz,
    rationale text,
    alternatives jsonb NOT NULL DEFAULT '[]'::jsonb,
    consequences text,
    rejection_reason text,
    review_at timestamptz,
    created_by_kind text NOT NULL,
    created_by_id text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, governing_project_id)
        REFERENCES radishnexus.projects (workspace_id, id),
    CONSTRAINT decisions_id_format CHECK (radishnexus.valid_entity_id('decision', id)),
    CONSTRAINT decisions_status CHECK (
        status IN ('proposed', 'accepted', 'rejected', 'superseded')
    ),
    CONSTRAINT decisions_deciders CHECK (array_position(decider_ids, NULL) IS NULL),
    CONSTRAINT decisions_alternatives CHECK (jsonb_typeof(alternatives) = 'array'),
    CONSTRAINT decisions_accepted_fields CHECK (
        status NOT IN ('accepted', 'superseded')
        OR (
            outcome IS NOT NULL
            AND rationale IS NOT NULL
            AND cardinality(decider_ids) > 0
            AND decided_at IS NOT NULL
        )
    ),
    CONSTRAINT decisions_rejected_fields CHECK (
        status <> 'rejected'
        OR (
            rejection_reason IS NOT NULL
            AND cardinality(decider_ids) > 0
            AND decided_at IS NOT NULL
        )
    ),
    CONSTRAINT decisions_actor CHECK (
        created_by_kind IN ('user', 'system', 'plugin', 'import')
        AND (created_by_kind = 'system' OR created_by_id IS NOT NULL)
    )
);

CREATE TABLE radishnexus.tickets (
    id text PRIMARY KEY,
    workspace_id text NOT NULL,
    governing_project_id text NOT NULL,
    title text NOT NULL,
    status text NOT NULL DEFAULT 'open',
    created_by text NOT NULL REFERENCES radishnexus.users (id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, governing_project_id)
        REFERENCES radishnexus.projects (workspace_id, id),
    CONSTRAINT tickets_id_format CHECK (radishnexus.valid_entity_id('ticket', id)),
    CONSTRAINT tickets_status CHECK (status IN ('open', 'in-progress', 'done', 'canceled'))
);

CREATE FUNCTION radishnexus.prevent_id_change()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    IF NEW.id <> OLD.id THEN
        RAISE EXCEPTION 'stable ID cannot change for %.%', TG_TABLE_NAME, OLD.id
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER users_id_immutable
BEFORE UPDATE ON radishnexus.users
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_id_change();

CREATE TRIGGER workspaces_id_immutable
BEFORE UPDATE ON radishnexus.workspaces
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_id_change();

CREATE FUNCTION radishnexus.prevent_workspace_identity_change()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    IF NEW.id <> OLD.id OR NEW.workspace_id <> OLD.workspace_id THEN
        RAISE EXCEPTION 'stable identity cannot change for %.%', TG_TABLE_NAME, OLD.id
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER teams_identity_immutable
BEFORE UPDATE ON radishnexus.teams
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_workspace_identity_change();

CREATE TRIGGER projects_identity_immutable
BEFORE UPDATE ON radishnexus.projects
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_workspace_identity_change();

CREATE FUNCTION radishnexus.prevent_scoped_identity_change()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    IF NEW.id <> OLD.id
       OR NEW.workspace_id <> OLD.workspace_id
       OR NEW.governing_project_id <> OLD.governing_project_id THEN
        RAISE EXCEPTION 'stable identity and governing Project cannot change for %.%', TG_TABLE_NAME, OLD.id
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER threads_scope_immutable
BEFORE UPDATE ON radishnexus.threads
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_scoped_identity_change();

CREATE TRIGGER decisions_scope_immutable
BEFORE UPDATE ON radishnexus.decisions
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_scoped_identity_change();

CREATE TRIGGER tickets_scope_immutable
BEFORE UPDATE ON radishnexus.tickets
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_scoped_identity_change();

CREATE TABLE radishnexus.domain_events (
    event_id text PRIMARY KEY,
    event_type text NOT NULL,
    schema_version integer NOT NULL,
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces (id),
    actor_kind text NOT NULL,
    actor_id text,
    source_kind text NOT NULL,
    source_id text,
    primary_entity_type text NOT NULL REFERENCES radishnexus.entity_types (type_name),
    primary_entity_id text NOT NULL,
    project_id text,
    correlation_id text NOT NULL,
    causation_id text,
    occurred_at timestamptz NOT NULL,
    payload jsonb NOT NULL,
    UNIQUE (workspace_id, event_id),
    FOREIGN KEY (workspace_id, project_id)
        REFERENCES radishnexus.projects (workspace_id, id),
    FOREIGN KEY (workspace_id, causation_id)
        REFERENCES radishnexus.domain_events (workspace_id, event_id)
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT domain_events_id_format CHECK (event_id LIKE 'evt_%'),
    CONSTRAINT domain_events_type_format CHECK (
        event_type ~ '^[a-z][a-z0-9-]*\.[a-z][a-z0-9-]*$'
    ),
    CONSTRAINT domain_events_schema_version CHECK (schema_version > 0),
    CONSTRAINT domain_events_actor CHECK (
        actor_kind IN ('user', 'system', 'plugin', 'import')
        AND (actor_kind = 'system' OR actor_id IS NOT NULL)
    ),
    CONSTRAINT domain_events_source CHECK (
        source_kind IN ('web', 'api', 'plugin', 'system', 'import')
        AND (source_kind NOT IN ('plugin', 'import') OR source_id IS NOT NULL)
    ),
    CONSTRAINT domain_events_primary_id CHECK (
        radishnexus.valid_entity_id(primary_entity_type, primary_entity_id)
    ),
    CONSTRAINT domain_events_payload CHECK (jsonb_typeof(payload) = 'object')
);

CREATE TABLE radishnexus.relation_types (
    from_type text NOT NULL REFERENCES radishnexus.entity_types (type_name),
    relation_type text NOT NULL,
    to_type text NOT NULL REFERENCES radishnexus.entity_types (type_name),
    PRIMARY KEY (from_type, relation_type, to_type),
    CONSTRAINT relation_types_name_format CHECK (
        relation_type ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$'
    )
);

INSERT INTO radishnexus.relation_types (from_type, relation_type, to_type) VALUES
    ('decision', 'derived-from', 'thread'),
    ('decision', 'supersedes', 'decision'),
    ('ticket', 'implements', 'decision');

CREATE TABLE radishnexus.entity_links (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces (id),
    from_type text NOT NULL,
    from_id text NOT NULL,
    relation_type text NOT NULL,
    to_type text NOT NULL,
    to_id text NOT NULL,
    assertion text NOT NULL,
    origin text NOT NULL,
    origin_ref text,
    created_by_kind text NOT NULL,
    created_by_id text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    source_event_id text,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    state text NOT NULL DEFAULT 'active',
    removed_by_kind text,
    removed_by_id text,
    removed_at timestamptz,
    removal_reason text,
    UNIQUE (workspace_id, id),
    FOREIGN KEY (from_type, relation_type, to_type)
        REFERENCES radishnexus.relation_types (from_type, relation_type, to_type),
    FOREIGN KEY (workspace_id, source_event_id)
        REFERENCES radishnexus.domain_events (workspace_id, event_id),
    CONSTRAINT entity_links_id_format CHECK (radishnexus.valid_entity_id('entity-link', id)),
    CONSTRAINT entity_links_endpoint_ids CHECK (
        radishnexus.valid_entity_id(from_type, from_id)
        AND radishnexus.valid_entity_id(to_type, to_id)
    ),
    CONSTRAINT entity_links_assertion CHECK (assertion IN ('asserted', 'derived')),
    CONSTRAINT entity_links_origin CHECK (origin IN ('user', 'system', 'plugin', 'import')),
    CONSTRAINT entity_links_actor CHECK (
        created_by_kind IN ('user', 'system', 'plugin', 'import')
        AND (created_by_kind = 'system' OR created_by_id IS NOT NULL)
    ),
    CONSTRAINT entity_links_metadata CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT entity_links_state CHECK (state IN ('active', 'removed')),
    CONSTRAINT entity_links_removal CHECK (
        (
            state = 'active'
            AND removed_by_kind IS NULL
            AND removed_by_id IS NULL
            AND removed_at IS NULL
            AND removal_reason IS NULL
        )
        OR (
            state = 'removed'
            AND removed_by_kind IN ('user', 'system', 'plugin', 'import')
            AND (removed_by_kind = 'system' OR removed_by_id IS NOT NULL)
            AND removed_at IS NOT NULL
            AND removal_reason IS NOT NULL
        )
    )
);

CREATE FUNCTION radishnexus.entity_workspace(entity_type text, entity_id text)
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
        WHEN 'thread' THEN
            SELECT workspace_id INTO resolved_workspace FROM threads WHERE id = entity_id;
        WHEN 'decision' THEN
            SELECT workspace_id INTO resolved_workspace FROM decisions WHERE id = entity_id;
        WHEN 'ticket' THEN
            SELECT workspace_id INTO resolved_workspace FROM tickets WHERE id = entity_id;
        ELSE
            RETURN NULL;
    END CASE;
    RETURN resolved_workspace;
END;
$$;

CREATE FUNCTION radishnexus.validate_entity_link_endpoints()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
DECLARE
    from_workspace text;
    to_workspace text;
BEGIN
    from_workspace := entity_workspace(NEW.from_type, NEW.from_id);
    to_workspace := entity_workspace(NEW.to_type, NEW.to_id);
    IF from_workspace IS NULL OR to_workspace IS NULL THEN
        RAISE EXCEPTION 'EntityLink endpoint does not exist'
            USING ERRCODE = '23503';
    END IF;
    IF from_workspace <> NEW.workspace_id OR to_workspace <> NEW.workspace_id THEN
        RAISE EXCEPTION 'EntityLink endpoints must share the link Workspace'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER entity_links_validate_endpoints
BEFORE INSERT ON radishnexus.entity_links
FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_entity_link_endpoints();

CREATE FUNCTION radishnexus.enforce_entity_link_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'EntityLink facts cannot be deleted'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.id <> OLD.id
       OR NEW.workspace_id <> OLD.workspace_id
       OR NEW.from_type <> OLD.from_type
       OR NEW.from_id <> OLD.from_id
       OR NEW.relation_type <> OLD.relation_type
       OR NEW.to_type <> OLD.to_type
       OR NEW.to_id <> OLD.to_id
       OR NEW.assertion <> OLD.assertion
       OR NEW.origin <> OLD.origin
       OR NEW.origin_ref IS DISTINCT FROM OLD.origin_ref
       OR NEW.created_by_kind <> OLD.created_by_kind
       OR NEW.created_by_id IS DISTINCT FROM OLD.created_by_id
       OR NEW.created_at <> OLD.created_at
       OR NEW.source_event_id IS DISTINCT FROM OLD.source_event_id
       OR NEW.metadata <> OLD.metadata THEN
        RAISE EXCEPTION 'EntityLink identity and provenance are immutable'
            USING ERRCODE = '23514';
    END IF;
    IF OLD.state = 'removed' THEN
        RAISE EXCEPTION 'removed EntityLink cannot transition again'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'EntityLink updated time cannot move backwards'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER entity_links_control_mutation
BEFORE UPDATE OR DELETE ON radishnexus.entity_links
FOR EACH ROW EXECUTE FUNCTION radishnexus.enforce_entity_link_mutation();

CREATE FUNCTION radishnexus.validate_decision_relations(
    checked_workspace_id text,
    checked_decision_id text
)
RETURNS void
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
DECLARE
    decision_status text;
BEGIN
    SELECT status INTO decision_status
    FROM decisions
    WHERE workspace_id = checked_workspace_id AND id = checked_decision_id;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM entity_links
        WHERE workspace_id = checked_workspace_id
          AND from_type = 'decision'
          AND from_id = checked_decision_id
          AND relation_type = 'derived-from'
          AND state = 'active'
    ) THEN
        RAISE EXCEPTION 'Decision % must retain at least one active evidence relation', checked_decision_id
            USING ERRCODE = '23514';
    END IF;
    IF decision_status = 'superseded' AND NOT EXISTS (
        SELECT 1
        FROM entity_links
        WHERE workspace_id = checked_workspace_id
          AND from_type = 'decision'
          AND relation_type = 'supersedes'
          AND to_type = 'decision'
          AND to_id = checked_decision_id
          AND state = 'active'
    ) THEN
        RAISE EXCEPTION 'superseded Decision % requires an active replacement relation', checked_decision_id
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE FUNCTION radishnexus.validate_decision_relations_from_row()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    PERFORM validate_decision_relations(NEW.workspace_id, NEW.id);
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER decisions_require_relations
AFTER INSERT OR UPDATE ON radishnexus.decisions
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_decision_relations_from_row();

CREATE FUNCTION radishnexus.validate_decision_relations_from_link()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    IF TG_OP <> 'DELETE' AND NEW.from_type = 'decision' THEN
        PERFORM validate_decision_relations(NEW.workspace_id, NEW.from_id);
    END IF;
    IF TG_OP <> 'INSERT' AND OLD.from_type = 'decision' THEN
        PERFORM validate_decision_relations(OLD.workspace_id, OLD.from_id);
    END IF;
    IF TG_OP <> 'DELETE' AND NEW.to_type = 'decision' THEN
        PERFORM validate_decision_relations(NEW.workspace_id, NEW.to_id);
    END IF;
    IF TG_OP <> 'INSERT' AND OLD.to_type = 'decision' THEN
        PERFORM validate_decision_relations(OLD.workspace_id, OLD.to_id);
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER entity_links_keep_decision_relations
AFTER INSERT OR UPDATE OR DELETE ON radishnexus.entity_links
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_decision_relations_from_link();

CREATE FUNCTION radishnexus.validate_event_primary_entity()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    IF entity_workspace(NEW.primary_entity_type, NEW.primary_entity_id) IS DISTINCT FROM NEW.workspace_id THEN
        RAISE EXCEPTION 'domain event primary entity must exist in event Workspace'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER domain_events_validate_primary_entity
AFTER INSERT ON radishnexus.domain_events
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_event_primary_entity();

CREATE FUNCTION radishnexus.prevent_event_fact_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    RAISE EXCEPTION 'domain event facts are immutable'
        USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER domain_events_immutable
BEFORE UPDATE OR DELETE ON radishnexus.domain_events
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_event_fact_mutation();

CREATE TABLE radishnexus.outbox_deliveries (
    event_id text NOT NULL REFERENCES radishnexus.domain_events (event_id),
    consumer text NOT NULL,
    state text NOT NULL DEFAULT 'pending',
    attempt_count integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    locked_until timestamptz,
    last_error text,
    delivered_at timestamptz,
    PRIMARY KEY (event_id, consumer),
    CONSTRAINT outbox_deliveries_state CHECK (
        state IN ('pending', 'processing', 'delivered', 'dead')
    ),
    CONSTRAINT outbox_deliveries_attempts CHECK (attempt_count >= 0),
    CONSTRAINT outbox_deliveries_delivered CHECK (
        (state = 'delivered' AND delivered_at IS NOT NULL)
        OR (state <> 'delivered' AND delivered_at IS NULL)
    )
);

CREATE INDEX outbox_deliveries_ready
ON radishnexus.outbox_deliveries (available_at, event_id)
WHERE state = 'pending';

CREATE INDEX entity_links_from_active
ON radishnexus.entity_links (workspace_id, from_type, from_id, created_at, id)
WHERE state = 'active';

CREATE INDEX project_memberships_user
ON radishnexus.project_memberships (workspace_id, user_id, project_id);

CREATE INDEX thread_memberships_user
ON radishnexus.thread_memberships (workspace_id, user_id, thread_id);

---- create above / drop below ----

DROP SCHEMA radishnexus CASCADE;
