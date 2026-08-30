INSERT INTO radishnexus.entity_types (type_name, id_prefix)
VALUES ('ci-run', 'cir_');

CREATE TABLE radishnexus.components (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces (id),
    key text NOT NULL,
    name text NOT NULL,
    summary text,
    type text NOT NULL,
    owner_team_id text,
    lifecycle text NOT NULL,
    created_by_kind text NOT NULL,
    created_by_id text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, key),
    FOREIGN KEY (workspace_id, owner_team_id)
        REFERENCES radishnexus.teams (workspace_id, id),
    CONSTRAINT components_id_format CHECK (
        radishnexus.valid_entity_id('component', id)
    ),
    CONSTRAINT components_key_nonempty CHECK (btrim(key) <> ''),
    CONSTRAINT components_name_nonempty CHECK (btrim(name) <> ''),
    CONSTRAINT components_type CHECK (
        type IN (
            'service', 'web', 'client', 'library',
            'data-pipeline', 'infrastructure', 'other'
        )
    ),
    CONSTRAINT components_lifecycle CHECK (
        lifecycle IN ('planned', 'active', 'deprecated', 'retired')
    ),
    CONSTRAINT components_active_owner CHECK (
        lifecycle = 'planned' OR owner_team_id IS NOT NULL
    ),
    CONSTRAINT components_actor CHECK (
        created_by_kind IN ('user', 'system', 'plugin', 'import')
        AND (created_by_kind = 'system' OR created_by_id IS NOT NULL)
    )
);

CREATE TRIGGER components_identity_immutable
BEFORE UPDATE ON radishnexus.components
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_workspace_identity_change();

CREATE TABLE radishnexus.ci_runs (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces (id),
    component_id text NOT NULL,
    source_kind text NOT NULL,
    source_id text NOT NULL,
    external_run_key text NOT NULL,
    status text NOT NULL,
    started_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, source_kind, source_id, external_run_key),
    FOREIGN KEY (workspace_id, component_id)
        REFERENCES radishnexus.components (workspace_id, id),
    CONSTRAINT ci_runs_id_format CHECK (
        radishnexus.valid_entity_id('ci-run', id)
    ),
    CONSTRAINT ci_runs_source_kind CHECK (source_kind = 'jenkins'),
    CONSTRAINT ci_runs_source_id CHECK (
        btrim(source_id) = source_id
        AND source_id <> ''
        AND char_length(source_id) <= 255
    ),
    CONSTRAINT ci_runs_external_run_key CHECK (
        btrim(external_run_key) = external_run_key
        AND external_run_key <> ''
        AND char_length(external_run_key) <= 512
    ),
    CONSTRAINT ci_runs_status CHECK (
        status IN ('queued', 'running', 'succeeded', 'failed', 'canceled')
    ),
    CONSTRAINT ci_runs_completion CHECK (
        (status IN ('queued', 'running') AND completed_at IS NULL)
        OR (status IN ('succeeded', 'failed', 'canceled') AND completed_at IS NOT NULL)
    ),
    CONSTRAINT ci_runs_time_order CHECK (
        started_at IS NULL OR completed_at IS NULL OR started_at <= completed_at
    )
);

CREATE FUNCTION radishnexus.prevent_ci_run_identity_change()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    IF NEW.id <> OLD.id
       OR NEW.workspace_id <> OLD.workspace_id
       OR NEW.component_id <> OLD.component_id
       OR NEW.source_kind <> OLD.source_kind
       OR NEW.source_id <> OLD.source_id
       OR NEW.external_run_key <> OLD.external_run_key THEN
        RAISE EXCEPTION 'CI Run identity, Component, and source mapping cannot change for %', OLD.id
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER ci_runs_identity_immutable
BEFORE UPDATE ON radishnexus.ci_runs
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_ci_run_identity_change();

CREATE TABLE radishnexus.inbound_deliveries (
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces (id),
    source_kind text NOT NULL,
    source_id text NOT NULL,
    delivery_id text NOT NULL,
    payload_sha256 text NOT NULL,
    ci_run_id text NOT NULL,
    event_id text NOT NULL,
    recorded_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, source_kind, source_id, delivery_id),
    FOREIGN KEY (workspace_id, ci_run_id)
        REFERENCES radishnexus.ci_runs (workspace_id, id)
        DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY (workspace_id, event_id)
        REFERENCES radishnexus.domain_events (workspace_id, event_id)
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT inbound_deliveries_source_kind CHECK (source_kind = 'jenkins'),
    CONSTRAINT inbound_deliveries_source_id CHECK (
        btrim(source_id) = source_id
        AND source_id <> ''
        AND char_length(source_id) <= 255
    ),
    CONSTRAINT inbound_deliveries_delivery_id CHECK (
        btrim(delivery_id) = delivery_id
        AND delivery_id <> ''
        AND char_length(delivery_id) <= 512
    ),
    CONSTRAINT inbound_deliveries_payload_sha256 CHECK (
        payload_sha256 ~ '^[0-9a-f]{64}$'
    )
);

CREATE FUNCTION radishnexus.prevent_inbound_delivery_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    RAISE EXCEPTION 'inbound delivery receipts are immutable'
        USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER inbound_deliveries_immutable
BEFORE UPDATE OR DELETE ON radishnexus.inbound_deliveries
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_inbound_delivery_mutation();

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
        WHEN 'thread' THEN
            SELECT workspace_id INTO resolved_workspace FROM threads WHERE id = entity_id;
        WHEN 'decision' THEN
            SELECT workspace_id INTO resolved_workspace FROM decisions WHERE id = entity_id;
        WHEN 'ticket' THEN
            SELECT workspace_id INTO resolved_workspace FROM tickets WHERE id = entity_id;
        WHEN 'ci-run' THEN
            SELECT workspace_id INTO resolved_workspace FROM ci_runs WHERE id = entity_id;
        ELSE
            RETURN NULL;
    END CASE;
    RETURN resolved_workspace;
END;
$$;

CREATE INDEX ci_runs_component_timeline
ON radishnexus.ci_runs (workspace_id, component_id, completed_at DESC, id);

CREATE INDEX inbound_deliveries_ci_run
ON radishnexus.inbound_deliveries (workspace_id, ci_run_id);

---- create above / drop below ----

DROP INDEX radishnexus.inbound_deliveries_ci_run;
DROP INDEX radishnexus.ci_runs_component_timeline;
DROP TRIGGER inbound_deliveries_immutable ON radishnexus.inbound_deliveries;
DROP FUNCTION radishnexus.prevent_inbound_delivery_mutation();
DROP TABLE radishnexus.inbound_deliveries;
DROP TRIGGER ci_runs_identity_immutable ON radishnexus.ci_runs;
DROP FUNCTION radishnexus.prevent_ci_run_identity_change();
DROP TABLE radishnexus.ci_runs;
DROP TRIGGER components_identity_immutable ON radishnexus.components;
DROP TABLE radishnexus.components;
DELETE FROM radishnexus.entity_types WHERE type_name = 'ci-run';
