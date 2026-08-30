CREATE SCHEMA m0_core;

CREATE TABLE m0_core.entity_types (
    type_name text PRIMARY KEY,
    id_prefix text,
    linkable boolean NOT NULL DEFAULT true,
    CONSTRAINT entity_types_name_format CHECK (
        type_name ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$'
    ),
    CONSTRAINT entity_types_prefix_format CHECK (
        id_prefix IS NULL OR id_prefix ~ '^[a-z][a-z0-9]*_$'
    )
);

INSERT INTO m0_core.entity_types (type_name, id_prefix) VALUES
    ('project', 'prj_'),
    ('initiative', 'ini_'),
    ('component', 'cmp_'),
    ('decision', 'dec_'),
    ('environment', 'env_'),
    ('entity-link', 'lnk_'),
    ('thread', NULL),
    ('ci-run', NULL);

CREATE FUNCTION m0_core.valid_entity_id(entity_type text, entity_id text)
RETURNS boolean
LANGUAGE sql
STABLE
SET search_path = m0_core, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM entity_types
        WHERE type_name = entity_type
          AND entity_id <> ''
          AND entity_id !~ '[/?#[:space:]]'
          AND octet_length(entity_id) = char_length(entity_id)
          AND (id_prefix IS NULL OR left(entity_id, length(id_prefix)) = id_prefix)
    );
$$;

CREATE TABLE m0_core.workspaces (
    id text PRIMARY KEY,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT workspaces_id_format CHECK (id LIKE 'wrk_%')
);

CREATE TABLE m0_core.projects (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES m0_core.workspaces (id),
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
    CONSTRAINT projects_id_format CHECK (m0_core.valid_entity_id('project', id)),
    CONSTRAINT projects_visibility CHECK (visibility IN ('workspace', 'restricted')),
    CONSTRAINT projects_status CHECK (status IN ('active', 'archived')),
    CONSTRAINT projects_actor CHECK (
        created_by_kind IN ('user', 'system', 'plugin', 'import')
        AND (created_by_kind = 'system' OR created_by_id IS NOT NULL)
    )
);

CREATE TABLE m0_core.initiatives (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES m0_core.workspaces (id),
    title text NOT NULL,
    summary text,
    desired_outcome text,
    owner_user_id text,
    owner_team_id text,
    status text NOT NULL,
    health text NOT NULL,
    start_at timestamptz,
    target_at timestamptz,
    completed_at timestamptz,
    created_by_kind text NOT NULL,
    created_by_id text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (workspace_id, id),
    CONSTRAINT initiatives_id_format CHECK (m0_core.valid_entity_id('initiative', id)),
    CONSTRAINT initiatives_status CHECK (
        status IN ('proposed', 'planned', 'active', 'completed', 'canceled')
    ),
    CONSTRAINT initiatives_health CHECK (
        health IN ('on-track', 'at-risk', 'off-track', 'unknown')
    ),
    CONSTRAINT initiatives_active_fields CHECK (
        status <> 'active'
        OR (
            desired_outcome IS NOT NULL
            AND owner_user_id IS NOT NULL
            AND owner_team_id IS NOT NULL
        )
    ),
    CONSTRAINT initiatives_completed_time CHECK (
        (status = 'completed' AND completed_at IS NOT NULL)
        OR (status <> 'completed' AND completed_at IS NULL)
    ),
    CONSTRAINT initiatives_actor CHECK (
        created_by_kind IN ('user', 'system', 'plugin', 'import')
        AND (created_by_kind = 'system' OR created_by_id IS NOT NULL)
    )
);

CREATE TABLE m0_core.components (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES m0_core.workspaces (id),
    key text NOT NULL,
    name text NOT NULL,
    summary text,
    component_type text NOT NULL,
    owner_team_id text,
    lifecycle text NOT NULL,
    created_by_kind text NOT NULL,
    created_by_id text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, key),
    CONSTRAINT components_id_format CHECK (m0_core.valid_entity_id('component', id)),
    CONSTRAINT components_type CHECK (
        component_type IN (
            'service',
            'web',
            'client',
            'library',
            'data-pipeline',
            'infrastructure',
            'other'
        )
    ),
    CONSTRAINT components_lifecycle CHECK (
        lifecycle IN ('planned', 'active', 'deprecated', 'retired')
    ),
    CONSTRAINT components_active_owner CHECK (
        lifecycle <> 'active' OR owner_team_id IS NOT NULL
    ),
    CONSTRAINT components_actor CHECK (
        created_by_kind IN ('user', 'system', 'plugin', 'import')
        AND (created_by_kind = 'system' OR created_by_id IS NOT NULL)
    )
);

CREATE TABLE m0_core.environments (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES m0_core.workspaces (id),
    key text NOT NULL,
    name text NOT NULL,
    classification text NOT NULL,
    owner_team_id text NOT NULL,
    status text NOT NULL,
    created_by_kind text NOT NULL,
    created_by_id text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, key),
    CONSTRAINT environments_id_format CHECK (m0_core.valid_entity_id('environment', id)),
    CONSTRAINT environments_classification CHECK (
        classification IN ('development', 'staging', 'production', 'other')
    ),
    CONSTRAINT environments_status CHECK (status IN ('active', 'archived')),
    CONSTRAINT environments_actor CHECK (
        created_by_kind IN ('user', 'system', 'plugin', 'import')
        AND (created_by_kind = 'system' OR created_by_id IS NOT NULL)
    )
);

-- Thread and CI Run are deliberately skeletal supporting records for the
-- experiment. Their complete field contracts and ID prefixes are not frozen.
CREATE TABLE m0_core.threads (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES m0_core.workspaces (id),
    visibility text NOT NULL,
    title text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (workspace_id, id),
    CONSTRAINT threads_id_format CHECK (m0_core.valid_entity_id('thread', id)),
    CONSTRAINT threads_visibility CHECK (visibility IN ('workspace', 'restricted'))
);

CREATE TABLE m0_core.ci_runs (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES m0_core.workspaces (id),
    integration_id text NOT NULL,
    external_run_key text NOT NULL,
    status text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, integration_id, external_run_key),
    CONSTRAINT ci_runs_id_format CHECK (m0_core.valid_entity_id('ci-run', id)),
    CONSTRAINT ci_runs_status CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'canceled'))
);

CREATE TABLE m0_core.decisions (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES m0_core.workspaces (id),
    question text NOT NULL,
    outcome text,
    status text NOT NULL,
    proposer_id text NOT NULL,
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
    CONSTRAINT decisions_id_format CHECK (m0_core.valid_entity_id('decision', id)),
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

CREATE FUNCTION m0_core.prevent_identity_change()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = m0_core, pg_temp
AS $$
BEGIN
    IF NEW.id <> OLD.id OR NEW.workspace_id <> OLD.workspace_id THEN
        RAISE EXCEPTION 'stable identity cannot change for %.%', TG_TABLE_NAME, OLD.id
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER projects_identity_immutable
BEFORE UPDATE ON m0_core.projects
FOR EACH ROW EXECUTE FUNCTION m0_core.prevent_identity_change();

CREATE TRIGGER initiatives_identity_immutable
BEFORE UPDATE ON m0_core.initiatives
FOR EACH ROW EXECUTE FUNCTION m0_core.prevent_identity_change();

CREATE TRIGGER components_identity_immutable
BEFORE UPDATE ON m0_core.components
FOR EACH ROW EXECUTE FUNCTION m0_core.prevent_identity_change();

CREATE TRIGGER environments_identity_immutable
BEFORE UPDATE ON m0_core.environments
FOR EACH ROW EXECUTE FUNCTION m0_core.prevent_identity_change();

CREATE TRIGGER threads_identity_immutable
BEFORE UPDATE ON m0_core.threads
FOR EACH ROW EXECUTE FUNCTION m0_core.prevent_identity_change();

CREATE TRIGGER ci_runs_identity_immutable
BEFORE UPDATE ON m0_core.ci_runs
FOR EACH ROW EXECUTE FUNCTION m0_core.prevent_identity_change();

CREATE TRIGGER decisions_identity_immutable
BEFORE UPDATE ON m0_core.decisions
FOR EACH ROW EXECUTE FUNCTION m0_core.prevent_identity_change();

CREATE TABLE m0_core.domain_events (
    event_id text PRIMARY KEY,
    event_type text NOT NULL,
    schema_version integer NOT NULL,
    workspace_id text NOT NULL REFERENCES m0_core.workspaces (id),
    actor_kind text NOT NULL,
    actor_id text,
    source_kind text NOT NULL,
    source_id text,
    primary_entity_type text NOT NULL REFERENCES m0_core.entity_types (type_name),
    primary_entity_id text NOT NULL,
    project_id text,
    correlation_id text NOT NULL,
    causation_id text,
    occurred_at timestamptz NOT NULL,
    payload jsonb NOT NULL,
    UNIQUE (workspace_id, event_id),
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
        m0_core.valid_entity_id(primary_entity_type, primary_entity_id)
    ),
    CONSTRAINT domain_events_payload CHECK (jsonb_typeof(payload) = 'object'),
    FOREIGN KEY (workspace_id, project_id)
        REFERENCES m0_core.projects (workspace_id, id),
    FOREIGN KEY (workspace_id, causation_id)
        REFERENCES m0_core.domain_events (workspace_id, event_id)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE m0_core.relation_types (
    from_type text NOT NULL REFERENCES m0_core.entity_types (type_name),
    relation_type text NOT NULL,
    to_type text NOT NULL REFERENCES m0_core.entity_types (type_name),
    PRIMARY KEY (from_type, relation_type, to_type),
    CONSTRAINT relation_types_name_format CHECK (
        relation_type ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$'
    )
);

INSERT INTO m0_core.relation_types (from_type, relation_type, to_type) VALUES
    ('decision', 'derived-from', 'thread'),
    ('decision', 'supersedes', 'decision');

CREATE TABLE m0_core.entity_links (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES m0_core.workspaces (id),
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
        REFERENCES m0_core.relation_types (from_type, relation_type, to_type),
    FOREIGN KEY (workspace_id, source_event_id)
        REFERENCES m0_core.domain_events (workspace_id, event_id),
    CONSTRAINT entity_links_id_format CHECK (m0_core.valid_entity_id('entity-link', id)),
    CONSTRAINT entity_links_endpoint_ids CHECK (
        m0_core.valid_entity_id(from_type, from_id)
        AND m0_core.valid_entity_id(to_type, to_id)
    ),
    CONSTRAINT entity_links_assertion CHECK (assertion IN ('asserted', 'derived')),
    CONSTRAINT entity_links_origin CHECK (origin IN ('user', 'system', 'plugin', 'import')),
    CONSTRAINT entity_links_origin_ref CHECK (
        origin = 'user' OR origin_ref IS NOT NULL
    ),
    CONSTRAINT entity_links_creator CHECK (
        created_by_kind IN ('user', 'system', 'plugin', 'import')
        AND (created_by_kind = 'system' OR created_by_id IS NOT NULL)
    ),
    CONSTRAINT entity_links_metadata CHECK (jsonb_typeof(metadata) = 'object'),
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

CREATE UNIQUE INDEX entity_links_source_event_once
ON m0_core.entity_links (
    workspace_id,
    source_event_id,
    from_type,
    from_id,
    relation_type,
    to_type,
    to_id
)
WHERE source_event_id IS NOT NULL;

CREATE FUNCTION m0_core.entity_exists(
    checked_workspace_id text,
    entity_type text,
    entity_id text
)
RETURNS boolean
LANGUAGE plpgsql
STABLE
SET search_path = m0_core, pg_temp
AS $$
BEGIN
    RETURN CASE entity_type
        WHEN 'project' THEN EXISTS (
            SELECT 1 FROM projects WHERE workspace_id = checked_workspace_id AND id = entity_id
        )
        WHEN 'initiative' THEN EXISTS (
            SELECT 1 FROM initiatives WHERE workspace_id = checked_workspace_id AND id = entity_id
        )
        WHEN 'component' THEN EXISTS (
            SELECT 1 FROM components WHERE workspace_id = checked_workspace_id AND id = entity_id
        )
        WHEN 'decision' THEN EXISTS (
            SELECT 1 FROM decisions WHERE workspace_id = checked_workspace_id AND id = entity_id
        )
        WHEN 'environment' THEN EXISTS (
            SELECT 1 FROM environments WHERE workspace_id = checked_workspace_id AND id = entity_id
        )
        WHEN 'entity-link' THEN EXISTS (
            SELECT 1 FROM entity_links WHERE workspace_id = checked_workspace_id AND id = entity_id
        )
        WHEN 'thread' THEN EXISTS (
            SELECT 1 FROM threads WHERE workspace_id = checked_workspace_id AND id = entity_id
        )
        WHEN 'ci-run' THEN EXISTS (
            SELECT 1 FROM ci_runs WHERE workspace_id = checked_workspace_id AND id = entity_id
        )
        ELSE false
    END;
END;
$$;

CREATE FUNCTION m0_core.validate_entity_link()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = m0_core, pg_temp
AS $$
BEGIN
    IF NOT entity_exists(NEW.workspace_id, NEW.from_type, NEW.from_id) THEN
        RAISE EXCEPTION 'EntityLink source is missing or belongs to another Workspace'
            USING ERRCODE = '23514';
    END IF;
    IF NOT entity_exists(NEW.workspace_id, NEW.to_type, NEW.to_id) THEN
        RAISE EXCEPTION 'EntityLink target is missing or belongs to another Workspace'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER entity_links_validate_endpoints
BEFORE INSERT OR UPDATE ON m0_core.entity_links
FOR EACH ROW EXECUTE FUNCTION m0_core.validate_entity_link();

CREATE FUNCTION m0_core.enforce_entity_link_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = m0_core, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'EntityLink records must be removed by state transition, not deleted'
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
    RETURN NEW;
END;
$$;

CREATE TRIGGER entity_links_control_mutation
BEFORE UPDATE OR DELETE ON m0_core.entity_links
FOR EACH ROW EXECUTE FUNCTION m0_core.enforce_entity_link_mutation();

CREATE FUNCTION m0_core.validate_decision_relations(
    checked_workspace_id text,
    decision_id text
)
RETURNS void
LANGUAGE plpgsql
SET search_path = m0_core, pg_temp
AS $$
DECLARE
    decision_status text;
BEGIN
    SELECT status INTO decision_status
    FROM decisions
    WHERE workspace_id = checked_workspace_id AND id = decision_id;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM entity_links
        WHERE workspace_id = checked_workspace_id
          AND from_type = 'decision'
          AND from_id = decision_id
          AND relation_type = 'derived-from'
          AND state = 'active'
    ) THEN
        RAISE EXCEPTION 'Decision % must retain at least one active evidence relation', decision_id
            USING ERRCODE = '23514';
    END IF;

    IF decision_status = 'superseded' AND NOT EXISTS (
        SELECT 1
        FROM entity_links
        WHERE workspace_id = checked_workspace_id
          AND from_type = 'decision'
          AND relation_type = 'supersedes'
          AND to_type = 'decision'
          AND to_id = decision_id
          AND state = 'active'
    ) THEN
        RAISE EXCEPTION 'superseded Decision % requires an active replacement relation', decision_id
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE FUNCTION m0_core.validate_decision_row_trigger()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = m0_core, pg_temp
AS $$
BEGIN
    PERFORM validate_decision_relations(NEW.workspace_id, NEW.id);
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER decisions_validate_relations
AFTER INSERT OR UPDATE ON m0_core.decisions
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION m0_core.validate_decision_row_trigger();

CREATE FUNCTION m0_core.validate_linked_decisions_trigger()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = m0_core, pg_temp
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

CREATE CONSTRAINT TRIGGER entity_links_validate_decisions
AFTER INSERT OR UPDATE OR DELETE ON m0_core.entity_links
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION m0_core.validate_linked_decisions_trigger();

CREATE TABLE m0_core.domain_event_targets (
    event_id text NOT NULL,
    workspace_id text NOT NULL,
    target_type text NOT NULL REFERENCES m0_core.entity_types (type_name),
    target_id text NOT NULL,
    role text NOT NULL,
    PRIMARY KEY (event_id, target_type, target_id),
    FOREIGN KEY (workspace_id, event_id)
        REFERENCES m0_core.domain_events (workspace_id, event_id),
    CONSTRAINT domain_event_targets_role CHECK (role IN ('primary', 'related')),
    CONSTRAINT domain_event_targets_id CHECK (
        m0_core.valid_entity_id(target_type, target_id)
    )
);

CREATE FUNCTION m0_core.validate_event_target()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = m0_core, pg_temp
AS $$
BEGIN
    IF NOT entity_exists(NEW.workspace_id, NEW.target_type, NEW.target_id) THEN
        RAISE EXCEPTION 'event target is missing or belongs to another Workspace'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER domain_event_targets_validate
BEFORE INSERT OR UPDATE ON m0_core.domain_event_targets
FOR EACH ROW EXECUTE FUNCTION m0_core.validate_event_target();

CREATE FUNCTION m0_core.validate_event_primary_target()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = m0_core, pg_temp
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM domain_event_targets
        WHERE event_id = NEW.event_id
          AND workspace_id = NEW.workspace_id
          AND target_type = NEW.primary_entity_type
          AND target_id = NEW.primary_entity_id
          AND role = 'primary'
    ) THEN
        RAISE EXCEPTION 'event % must include its primary entity as a primary target', NEW.event_id
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER domain_events_validate_primary_target
AFTER INSERT ON m0_core.domain_events
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION m0_core.validate_event_primary_target();

CREATE FUNCTION m0_core.prevent_event_fact_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = m0_core, pg_temp
AS $$
BEGIN
    RAISE EXCEPTION 'domain event facts are immutable'
        USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER domain_events_immutable
BEFORE UPDATE OR DELETE ON m0_core.domain_events
FOR EACH ROW EXECUTE FUNCTION m0_core.prevent_event_fact_mutation();

CREATE TRIGGER domain_event_targets_immutable
BEFORE UPDATE OR DELETE ON m0_core.domain_event_targets
FOR EACH ROW EXECUTE FUNCTION m0_core.prevent_event_fact_mutation();

CREATE TABLE m0_core.outbox_deliveries (
    event_id text NOT NULL REFERENCES m0_core.domain_events (event_id),
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
ON m0_core.outbox_deliveries (available_at, event_id)
WHERE state = 'pending';

CREATE TABLE m0_core.inbound_deliveries (
    workspace_id text NOT NULL REFERENCES m0_core.workspaces (id),
    source_kind text NOT NULL,
    source_id text NOT NULL,
    delivery_id text NOT NULL,
    state text NOT NULL DEFAULT 'processing',
    received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    processed_at timestamptz,
    processed_event_id text,
    PRIMARY KEY (workspace_id, source_kind, source_id, delivery_id),
    FOREIGN KEY (workspace_id, processed_event_id)
        REFERENCES m0_core.domain_events (workspace_id, event_id),
    CONSTRAINT inbound_deliveries_state CHECK (state IN ('processing', 'processed', 'failed')),
    CONSTRAINT inbound_deliveries_processed CHECK (
        (
            state = 'processed'
            AND processed_at IS NOT NULL
            AND processed_event_id IS NOT NULL
        )
        OR state <> 'processed'
    )
);

CREATE TABLE m0_core.activities (
    workspace_id text NOT NULL REFERENCES m0_core.workspaces (id),
    target_type text NOT NULL REFERENCES m0_core.entity_types (type_name),
    target_id text NOT NULL,
    event_id text NOT NULL REFERENCES m0_core.domain_events (event_id),
    activity_type text NOT NULL,
    actor_kind text NOT NULL,
    actor_id text,
    occurred_at timestamptz NOT NULL,
    subject_refs jsonb NOT NULL,
    projection_version integer NOT NULL,
    safe_facts jsonb NOT NULL,
    PRIMARY KEY (projection_version, event_id, target_type, target_id),
    CONSTRAINT activities_target_id CHECK (
        m0_core.valid_entity_id(target_type, target_id)
    ),
    CONSTRAINT activities_subject_refs CHECK (jsonb_typeof(subject_refs) = 'array'),
    CONSTRAINT activities_projection_version CHECK (projection_version > 0),
    CONSTRAINT activities_safe_facts CHECK (jsonb_typeof(safe_facts) = 'object')
);

CREATE INDEX activities_timeline
ON m0_core.activities (
    workspace_id,
    target_type,
    target_id,
    occurred_at DESC,
    event_id DESC
);

CREATE FUNCTION m0_core.safe_activity_facts(event_type text, payload jsonb)
RETURNS jsonb
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT CASE event_type
        WHEN 'decision.proposed' THEN jsonb_build_object('status', 'proposed')
        WHEN 'decision.accepted' THEN jsonb_build_object('status', 'accepted')
        WHEN 'decision.rejected' THEN jsonb_build_object('status', 'rejected')
        WHEN 'decision.superseded' THEN jsonb_build_object('status', 'superseded')
        WHEN 'entity-link.created' THEN jsonb_build_object('state', 'active')
        WHEN 'entity-link.removed' THEN jsonb_build_object('state', 'removed')
        WHEN 'ci-run.recorded' THEN jsonb_build_object('status', payload ->> 'status')
        ELSE NULL
    END;
$$;

CREATE FUNCTION m0_core.rebuild_activities(requested_projection_version integer)
RETURNS integer
LANGUAGE plpgsql
SET search_path = m0_core, pg_temp
AS $$
DECLARE
    projected_count integer;
BEGIN
    IF requested_projection_version <= 0 THEN
        RAISE EXCEPTION 'projection version must be positive'
            USING ERRCODE = '22023';
    END IF;

    DELETE FROM activities
    WHERE projection_version = requested_projection_version;

    INSERT INTO activities (
        workspace_id,
        target_type,
        target_id,
        event_id,
        activity_type,
        actor_kind,
        actor_id,
        occurred_at,
        subject_refs,
        projection_version,
        safe_facts
    )
    SELECT
        event.workspace_id,
        target.target_type,
        target.target_id,
        event.event_id,
        event.event_type,
        event.actor_kind,
        event.actor_id,
        event.occurred_at,
        (
            SELECT COALESCE(
                jsonb_agg(
                    jsonb_build_object('type', subject.target_type, 'id', subject.target_id)
                    ORDER BY subject.target_type, subject.target_id
                ) FILTER (
                    WHERE subject.target_type <> target.target_type
                       OR subject.target_id <> target.target_id
                ),
                '[]'::jsonb
            )
            FROM domain_event_targets AS subject
            WHERE subject.event_id = event.event_id
        ),
        requested_projection_version,
        safe_activity_facts(event.event_type, event.payload)
    FROM domain_events AS event
    JOIN domain_event_targets AS target ON target.event_id = event.event_id
    WHERE safe_activity_facts(event.event_type, event.payload) IS NOT NULL;

    GET DIAGNOSTICS projected_count = ROW_COUNT;
    RETURN projected_count;
END;
$$;
