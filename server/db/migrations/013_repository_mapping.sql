INSERT INTO radishnexus.entity_types(type_name,id_prefix) VALUES ('repository','rep_');

CREATE TABLE radishnexus.repositories (
    id text PRIMARY KEY CHECK (radishnexus.valid_entity_id('repository',id)),
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120 AND octet_length(name)<=480 AND name=btrim(name) AND name !~ '[[:cntrl:]]'),
    provider text NOT NULL CHECK (provider IN ('github','gitlab','gitea')),
    provider_origin text NOT NULL CHECK (octet_length(provider_origin) BETWEEN 1 AND 512 AND provider_origin ~ '^https://[^/?#[:space:]]+$'),
    external_id text NOT NULL CHECK (octet_length(external_id) BETWEEN 1 AND 255 AND external_id !~ '[[:space:][:cntrl:]]'),
    web_url text NOT NULL CHECK (octet_length(web_url) BETWEEN 1 AND 2048 AND starts_with(web_url,provider_origin || '/') AND length(web_url)>length(provider_origin)+1 AND web_url !~ '[?#[:space:][:cntrl:]]'),
    default_branch text NOT NULL CHECK (octet_length(default_branch) BETWEEN 1 AND 255 AND default_branch ~ '^[A-Za-z0-9][A-Za-z0-9._/-]*$'),
    created_by_kind text NOT NULL CHECK (created_by_kind='user'),
    created_by_id text NOT NULL REFERENCES radishnexus.users(id),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL CHECK (updated_at=created_at),
    UNIQUE(workspace_id,id),
    UNIQUE(workspace_id,provider,provider_origin,external_id)
);

CREATE FUNCTION radishnexus.prevent_repository_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Repository mapping metadata is immutable' USING ERRCODE='23514';
END;
$$;
CREATE TRIGGER repositories_immutable BEFORE UPDATE OR DELETE ON radishnexus.repositories
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_repository_mutation();

CREATE OR REPLACE FUNCTION radishnexus.entity_workspace(entity_type text, entity_id text)
RETURNS text LANGUAGE plpgsql STABLE SET search_path = radishnexus, pg_temp AS $$
DECLARE resolved_workspace text;
BEGIN
    CASE entity_type
        WHEN 'project' THEN SELECT workspace_id INTO resolved_workspace FROM projects WHERE id=entity_id;
        WHEN 'component' THEN SELECT workspace_id INTO resolved_workspace FROM components WHERE id=entity_id;
        WHEN 'repository' THEN SELECT workspace_id INTO resolved_workspace FROM repositories WHERE id=entity_id;
        WHEN 'environment' THEN SELECT workspace_id INTO resolved_workspace FROM environments WHERE id=entity_id;
        WHEN 'channel' THEN SELECT workspace_id INTO resolved_workspace FROM channels WHERE id=entity_id;
        WHEN 'message' THEN SELECT workspace_id INTO resolved_workspace FROM messages WHERE id=entity_id;
        WHEN 'thread' THEN SELECT workspace_id INTO resolved_workspace FROM threads WHERE id=entity_id;
        WHEN 'decision' THEN SELECT workspace_id INTO resolved_workspace FROM decisions WHERE id=entity_id;
        WHEN 'document' THEN SELECT workspace_id INTO resolved_workspace FROM documents WHERE id=entity_id;
        WHEN 'ticket' THEN SELECT workspace_id INTO resolved_workspace FROM tickets WHERE id=entity_id;
        WHEN 'ci-run' THEN SELECT workspace_id INTO resolved_workspace FROM ci_runs WHERE id=entity_id;
        WHEN 'deployment' THEN SELECT workspace_id INTO resolved_workspace FROM deployments WHERE id=entity_id;
        ELSE RETURN NULL;
    END CASE;
    RETURN resolved_workspace;
END;
$$;

INSERT INTO radishnexus.relation_types(from_type,relation_type,to_type)
VALUES ('component','source-repository','repository');
ALTER TABLE radishnexus.entity_links ADD CONSTRAINT repository_link_provenance CHECK (
    relation_type <> 'source-repository' OR (
        from_type='component' AND to_type='repository' AND assertion='asserted'
        AND origin='user' AND origin_ref IS NULL AND created_by_kind='user'
        AND source_event_id IS NULL AND metadata='{}'::jsonb
        AND (state='active' OR (removed_by_kind='user' AND removal_reason='owner-unlinked'))
    )
);
CREATE UNIQUE INDEX component_repository_active ON radishnexus.entity_links(workspace_id,from_id,to_id)
WHERE from_type='component' AND relation_type='source-repository' AND to_type='repository' AND state='active';
CREATE INDEX repository_components_active ON radishnexus.entity_links(workspace_id,to_id,from_id)
WHERE from_type='component' AND relation_type='source-repository' AND to_type='repository' AND state='active';

ALTER TABLE radishnexus.workspace_configuration_audit
    DROP CONSTRAINT configuration_audit_scope,
    DROP CONSTRAINT configuration_audit_result,
    DROP CONSTRAINT configuration_audit_state,
    ADD CONSTRAINT configuration_audit_scope CHECK (
        (command_kind IN ('team.create','project.create','component.create','environment.create','repository.create') AND scope_id=workspace_id AND subject_id='')
        OR (command_kind='channel.create' AND scope_id LIKE 'prj_%' AND subject_id='')
        OR (command_kind IN ('project.member.set','project.member.remove') AND scope_id LIKE 'prj_%' AND subject_id LIKE 'usr_%')
        OR (command_kind IN ('channel.member.add','channel.member.remove') AND scope_id LIKE 'chn_%' AND subject_id LIKE 'usr_%')
        OR (command_kind IN ('environment.authorization.grant','environment.authorization.revoke') AND scope_id LIKE 'env_%' AND subject_id LIKE 'usr_%')
        OR (command_kind='component.repository.link' AND radishnexus.valid_entity_id('component',scope_id) AND radishnexus.valid_entity_id('repository',subject_id))
        OR (command_kind='component.repository.unlink' AND radishnexus.valid_entity_id('component',scope_id) AND radishnexus.valid_entity_id('entity-link',subject_id))
    ),
    ADD CONSTRAINT configuration_audit_result CHECK (
        (command_kind='team.create' AND result_id LIKE 'tem_%')
        OR (command_kind='project.create' AND result_id LIKE 'prj_%')
        OR (command_kind='channel.create' AND result_id LIKE 'chn_%')
        OR (command_kind='component.create' AND result_id LIKE 'cmp_%')
        OR (command_kind='environment.create' AND result_id LIKE 'env_%')
        OR (command_kind='repository.create' AND radishnexus.valid_entity_id('repository',result_id))
        OR (command_kind='component.repository.link' AND radishnexus.valid_entity_id('entity-link',result_id))
        OR (command_kind='component.repository.unlink' AND result_id=subject_id)
        OR (command_kind IN ('project.member.set','project.member.remove','channel.member.add','channel.member.remove','environment.authorization.grant','environment.authorization.revoke') AND result_id=subject_id)
    ),
    ADD CONSTRAINT configuration_audit_state CHECK (
        (command_kind IN ('team.create','project.create','channel.create','project.member.set','project.member.remove','channel.member.add','channel.member.remove')
            AND before_state IN ('','viewer','contributor','decider','admin','member')
            AND after_state IN ('','viewer','contributor','decider','admin','member') AND authorization_id IS NULL)
        OR (command_kind IN ('component.create','environment.create','repository.create') AND before_state='' AND after_state='' AND changed
            AND authorization_id IS NULL AND cardinality(granted_user_ids)=0 AND cardinality(removed_channel_ids)=0 AND cardinality(removed_thread_ids)=0)
        OR (command_kind IN ('component.repository.link','component.repository.unlink') AND changed
            AND authorization_id IS NULL AND cardinality(granted_user_ids)=0 AND cardinality(removed_channel_ids)=0 AND cardinality(removed_thread_ids)=0
            AND ((command_kind='component.repository.link' AND before_state='' AND after_state='active')
                OR (command_kind='component.repository.unlink' AND before_state='active' AND after_state='removed')))
        OR (command_kind IN ('environment.authorization.grant','environment.authorization.revoke')
            AND cardinality(granted_user_ids)=0 AND cardinality(removed_channel_ids)=0 AND cardinality(removed_thread_ids)=0
            AND (
                (command_kind='environment.authorization.grant' AND authorization_id IS NOT NULL AND after_state='active'
                    AND ((before_state IN ('','revoked') AND changed) OR (before_state='active' AND NOT changed)))
                OR (command_kind='environment.authorization.revoke'
                    AND ((before_state='active' AND after_state='revoked' AND changed AND authorization_id IS NOT NULL)
                        OR (before_state='revoked' AND after_state='revoked' AND NOT changed AND authorization_id IS NOT NULL)
                        OR (before_state='' AND after_state='' AND NOT changed AND authorization_id IS NULL)))
            ))
    );

ALTER TABLE radishnexus.workspace_configuration_receipts
    DROP CONSTRAINT configuration_receipt_scope,
    ADD CONSTRAINT configuration_receipt_scope CHECK (
        (command_kind IN ('team.create','project.create','component.create','environment.create','repository.create') AND scope_id=workspace_id AND subject_id='')
        OR (command_kind='channel.create' AND scope_id LIKE 'prj_%' AND subject_id='')
        OR (command_kind IN ('project.member.set','project.member.remove') AND scope_id LIKE 'prj_%' AND subject_id LIKE 'usr_%')
        OR (command_kind IN ('channel.member.add','channel.member.remove') AND scope_id LIKE 'chn_%' AND subject_id LIKE 'usr_%')
        OR (command_kind IN ('environment.authorization.grant','environment.authorization.revoke') AND scope_id LIKE 'env_%' AND subject_id LIKE 'usr_%')
        OR (command_kind='component.repository.link' AND radishnexus.valid_entity_id('component',scope_id) AND radishnexus.valid_entity_id('repository',subject_id))
        OR (command_kind='component.repository.unlink' AND radishnexus.valid_entity_id('component',scope_id) AND radishnexus.valid_entity_id('entity-link',subject_id))
    );

CREATE FUNCTION radishnexus.validate_repository_configuration_evidence()
RETURNS trigger LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF NEW.command_kind='repository.create' AND NOT EXISTS (
        SELECT 1 FROM repositories WHERE workspace_id=NEW.workspace_id AND id=NEW.result_id
        AND created_by_id=NEW.actor_id AND created_at=NEW.occurred_at
    ) THEN
        RAISE EXCEPTION 'Repository Audit must identify its created mapping' USING ERRCODE='23514';
    END IF;
    IF NEW.command_kind IN ('component.repository.link','component.repository.unlink') AND NOT EXISTS (
        SELECT 1 FROM entity_links WHERE workspace_id=NEW.workspace_id AND id=NEW.result_id
        AND from_type='component' AND from_id=NEW.scope_id AND to_type='repository' AND relation_type='source-repository'
        AND ((NEW.command_kind='component.repository.link' AND to_id=NEW.subject_id AND state='active' AND created_by_id=NEW.actor_id AND created_at=NEW.occurred_at)
            OR (NEW.command_kind='component.repository.unlink' AND id=NEW.subject_id AND state='removed' AND removed_by_id=NEW.actor_id AND removed_at=NEW.occurred_at))
    ) THEN
        RAISE EXCEPTION 'Repository relation Audit must identify its exact operation' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER repository_configuration_evidence BEFORE INSERT ON radishnexus.workspace_configuration_audit
FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_repository_configuration_evidence();

-- Prior event shapes are unchanged by the additive v4 projection contract.
UPDATE radishnexus.activity_items SET projection_version=4 WHERE projection_version=3;

---- create above / drop below ----
-- Forward-only. Restore the pre-upgrade backup to a new target for version rollback.
