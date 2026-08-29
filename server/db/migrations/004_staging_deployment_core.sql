INSERT INTO radishnexus.entity_types (type_name, id_prefix)
VALUES ('deployment', 'dpl_');

CREATE TABLE radishnexus.environments (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces (id),
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
    FOREIGN KEY (workspace_id, owner_team_id)
        REFERENCES radishnexus.teams (workspace_id, id),
    CONSTRAINT environments_id_format CHECK (
        radishnexus.valid_entity_id('environment', id)
    ),
    CONSTRAINT environments_key_nonempty CHECK (btrim(key) <> ''),
    CONSTRAINT environments_name_nonempty CHECK (btrim(name) <> ''),
    CONSTRAINT environments_classification CHECK (
        classification IN ('development', 'staging', 'production', 'other')
    ),
    CONSTRAINT environments_status CHECK (status IN ('active', 'archived')),
    CONSTRAINT environments_actor CHECK (
        created_by_kind IN ('user', 'system', 'plugin', 'import')
        AND (created_by_kind = 'system' OR created_by_id IS NOT NULL)
    )
);

CREATE FUNCTION radishnexus.prevent_environment_identity_change()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    IF NEW.id <> OLD.id
       OR NEW.workspace_id <> OLD.workspace_id
       OR NEW.classification <> OLD.classification THEN
        RAISE EXCEPTION 'Environment identity and classification cannot change for %', OLD.id
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER environments_identity_immutable
BEFORE UPDATE ON radishnexus.environments
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_environment_identity_change();

CREATE TABLE radishnexus.environment_deployment_authorizations (
    id text PRIMARY KEY,
    workspace_id text NOT NULL,
    environment_id text NOT NULL,
    user_id text NOT NULL,
    status text NOT NULL DEFAULT 'active',
    granted_by text NOT NULL,
    granted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    revoked_by text,
    revoked_at timestamptz,
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, environment_id, user_id),
    UNIQUE (workspace_id, id, environment_id, user_id),
    FOREIGN KEY (workspace_id, environment_id)
        REFERENCES radishnexus.environments (workspace_id, id),
    FOREIGN KEY (workspace_id, user_id)
        REFERENCES radishnexus.workspace_memberships (workspace_id, user_id),
    FOREIGN KEY (workspace_id, granted_by)
        REFERENCES radishnexus.workspace_memberships (workspace_id, user_id),
    FOREIGN KEY (workspace_id, revoked_by)
        REFERENCES radishnexus.workspace_memberships (workspace_id, user_id),
    CONSTRAINT environment_deployment_authorizations_id_format CHECK (
        id LIKE 'dpa_%'
    ),
    CONSTRAINT environment_deployment_authorizations_status CHECK (
        status IN ('active', 'revoked')
    ),
    CONSTRAINT environment_deployment_authorizations_revocation CHECK (
        (status = 'active' AND revoked_by IS NULL AND revoked_at IS NULL)
        OR (status = 'revoked' AND revoked_by IS NOT NULL AND revoked_at IS NOT NULL)
    )
);

CREATE FUNCTION radishnexus.enforce_environment_deployment_authorization_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Environment deployment authorizations cannot be deleted'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.id <> OLD.id
       OR NEW.workspace_id <> OLD.workspace_id
       OR NEW.environment_id <> OLD.environment_id
       OR NEW.user_id <> OLD.user_id
       OR NEW.granted_by <> OLD.granted_by
       OR NEW.granted_at <> OLD.granted_at THEN
        RAISE EXCEPTION 'Environment deployment authorization provenance is immutable'
            USING ERRCODE = '23514';
    END IF;
    IF OLD.status = 'revoked' THEN
        RAISE EXCEPTION 'revoked Environment deployment authorization cannot transition again'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER environment_deployment_authorizations_control_mutation
BEFORE UPDATE OR DELETE ON radishnexus.environment_deployment_authorizations
FOR EACH ROW EXECUTE FUNCTION radishnexus.enforce_environment_deployment_authorization_mutation();

CREATE TABLE radishnexus.deployments (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces (id),
    environment_id text NOT NULL,
    ci_run_id text NOT NULL,
    authorization_id text NOT NULL,
    status text NOT NULL,
    started_at timestamptz,
    completed_at timestamptz NOT NULL,
    recorded_by text NOT NULL,
    source_kind text NOT NULL,
    source_id text,
    recorded_at timestamptz NOT NULL,
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, environment_id, ci_run_id),
    FOREIGN KEY (workspace_id, environment_id)
        REFERENCES radishnexus.environments (workspace_id, id),
    FOREIGN KEY (workspace_id, ci_run_id)
        REFERENCES radishnexus.ci_runs (workspace_id, id),
    FOREIGN KEY (workspace_id, authorization_id, environment_id, recorded_by)
        REFERENCES radishnexus.environment_deployment_authorizations (
            workspace_id, id, environment_id, user_id
        ),
    CONSTRAINT deployments_id_format CHECK (
        radishnexus.valid_entity_id('deployment', id)
    ),
    CONSTRAINT deployments_status CHECK (
        status IN ('succeeded', 'failed', 'canceled')
    ),
    CONSTRAINT deployments_time_order CHECK (
        started_at IS NULL OR started_at <= completed_at
    ),
    CONSTRAINT deployments_source CHECK (
        source_kind IN ('web', 'api')
    )
);

CREATE FUNCTION radishnexus.validate_staging_deployment_context()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
DECLARE
    environment_classification text;
    environment_status text;
    ci_run_status text;
    authorization_status text;
    membership_status text;
BEGIN
    SELECT classification, status
    INTO environment_classification, environment_status
    FROM environments
    WHERE workspace_id = NEW.workspace_id AND id = NEW.environment_id;

    SELECT status
    INTO ci_run_status
    FROM ci_runs
    WHERE workspace_id = NEW.workspace_id AND id = NEW.ci_run_id;

    SELECT status
    INTO authorization_status
    FROM environment_deployment_authorizations
    WHERE workspace_id = NEW.workspace_id
      AND id = NEW.authorization_id
      AND environment_id = NEW.environment_id
      AND user_id = NEW.recorded_by;

    SELECT status
    INTO membership_status
    FROM workspace_memberships
    WHERE workspace_id = NEW.workspace_id AND user_id = NEW.recorded_by;

    IF environment_classification IS DISTINCT FROM 'staging'
       OR environment_status IS DISTINCT FROM 'active' THEN
        RAISE EXCEPTION 'Deployment target must be an active staging Environment'
            USING ERRCODE = '23514';
    END IF;
    IF ci_run_status IS DISTINCT FROM 'succeeded' THEN
        RAISE EXCEPTION 'staging Deployment requires a succeeded CI Run'
            USING ERRCODE = '23514';
    END IF;
    IF authorization_status IS DISTINCT FROM 'active'
       OR membership_status IS DISTINCT FROM 'active' THEN
        RAISE EXCEPTION 'staging Deployment requires active explicit authorization'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER deployments_validate_staging_context
BEFORE INSERT ON radishnexus.deployments
FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_staging_deployment_context();

CREATE FUNCTION radishnexus.prevent_deployment_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    RAISE EXCEPTION 'completed Deployment facts are immutable'
        USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER deployments_immutable
BEFORE UPDATE OR DELETE ON radishnexus.deployments
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_deployment_mutation();

INSERT INTO radishnexus.relation_types (from_type, relation_type, to_type)
VALUES ('deployment', 'deploys', 'ci-run');

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

CREATE INDEX deployments_environment_timeline
ON radishnexus.deployments (workspace_id, environment_id, completed_at DESC, id);

CREATE INDEX deployments_ci_run
ON radishnexus.deployments (workspace_id, ci_run_id);

---- create above / drop below ----

DROP INDEX radishnexus.deployments_ci_run;
DROP INDEX radishnexus.deployments_environment_timeline;
DELETE FROM radishnexus.relation_types
WHERE from_type = 'deployment' AND relation_type = 'deploys' AND to_type = 'ci-run';
DROP TRIGGER deployments_immutable ON radishnexus.deployments;
DROP FUNCTION radishnexus.prevent_deployment_mutation();
DROP TRIGGER deployments_validate_staging_context ON radishnexus.deployments;
DROP FUNCTION radishnexus.validate_staging_deployment_context();
DROP TABLE radishnexus.deployments;
DROP TRIGGER environment_deployment_authorizations_control_mutation
ON radishnexus.environment_deployment_authorizations;
DROP FUNCTION radishnexus.enforce_environment_deployment_authorization_mutation();
DROP TABLE radishnexus.environment_deployment_authorizations;
DROP TRIGGER environments_identity_immutable ON radishnexus.environments;
DROP FUNCTION radishnexus.prevent_environment_identity_change();
DROP TABLE radishnexus.environments;
DELETE FROM radishnexus.entity_types WHERE type_name = 'deployment';
