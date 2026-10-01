ALTER TABLE radishnexus.environment_deployment_authorizations
    ADD COLUMN generation bigint NOT NULL DEFAULT 1 CHECK (generation > 0);

-- Resolve the original automatically named constraint by its exact columns.
DO $$
DECLARE original_name text;
BEGIN
    SELECT c.conname INTO STRICT original_name
    FROM pg_constraint c
    WHERE c.conrelid = 'radishnexus.environment_deployment_authorizations'::regclass
      AND c.contype = 'u'
      AND (SELECT array_agg(a.attname::text ORDER BY k.ordinality)
           FROM unnest(c.conkey) WITH ORDINALITY k(attnum, ordinality)
           JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.attnum)
          = ARRAY['workspace_id','environment_id','user_id'];
    EXECUTE format('ALTER TABLE radishnexus.environment_deployment_authorizations DROP CONSTRAINT %I', original_name);
END;
$$;

CREATE UNIQUE INDEX environment_authorization_active
ON radishnexus.environment_deployment_authorizations(workspace_id,environment_id,user_id)
WHERE status='active';
ALTER TABLE radishnexus.environment_deployment_authorizations
    ADD CONSTRAINT environment_authorization_generation UNIQUE(workspace_id,environment_id,user_id,generation);

CREATE OR REPLACE FUNCTION radishnexus.enforce_environment_deployment_authorization_mutation()
RETURNS trigger LANGUAGE plpgsql SET search_path = radishnexus, pg_temp AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Environment deployment authorizations cannot be deleted' USING ERRCODE='23514';
    END IF;
    IF NEW.id <> OLD.id OR NEW.workspace_id <> OLD.workspace_id
       OR NEW.environment_id <> OLD.environment_id OR NEW.user_id <> OLD.user_id
       OR NEW.granted_by <> OLD.granted_by OR NEW.granted_at <> OLD.granted_at
       OR NEW.generation <> OLD.generation THEN
        RAISE EXCEPTION 'Environment deployment authorization provenance is immutable' USING ERRCODE='23514';
    END IF;
    IF OLD.status = 'revoked' THEN
        RAISE EXCEPTION 'revoked Environment deployment authorization cannot transition again' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE radishnexus.workspace_configuration_audit
    ADD COLUMN authorization_id text,
    ADD CONSTRAINT configuration_authorization_context
        FOREIGN KEY(workspace_id,authorization_id,scope_id,subject_id)
        REFERENCES radishnexus.environment_deployment_authorizations(workspace_id,id,environment_id,user_id),
    DROP CONSTRAINT workspace_configuration_audit_command_kind_check,
    DROP CONSTRAINT workspace_configuration_audit_before_state_check,
    DROP CONSTRAINT workspace_configuration_audit_after_state_check,
    DROP CONSTRAINT workspace_configuration_audit_check,
    DROP CONSTRAINT workspace_configuration_audit_check1;

ALTER TABLE radishnexus.workspace_configuration_audit
    ADD CONSTRAINT configuration_audit_scope CHECK (
        (command_kind IN ('team.create','project.create','component.create','environment.create') AND scope_id=workspace_id AND subject_id='')
        OR (command_kind='channel.create' AND scope_id LIKE 'prj_%' AND subject_id='')
        OR (command_kind IN ('project.member.set','project.member.remove') AND scope_id LIKE 'prj_%' AND subject_id LIKE 'usr_%')
        OR (command_kind IN ('channel.member.add','channel.member.remove') AND scope_id LIKE 'chn_%' AND subject_id LIKE 'usr_%')
        OR (command_kind IN ('environment.authorization.grant','environment.authorization.revoke') AND scope_id LIKE 'env_%' AND subject_id LIKE 'usr_%')
    ),
    ADD CONSTRAINT configuration_audit_result CHECK (
        (command_kind='team.create' AND result_id LIKE 'tem_%')
        OR (command_kind='project.create' AND result_id LIKE 'prj_%')
        OR (command_kind='channel.create' AND result_id LIKE 'chn_%')
        OR (command_kind='component.create' AND result_id LIKE 'cmp_%')
        OR (command_kind='environment.create' AND result_id LIKE 'env_%')
        OR (command_kind IN ('project.member.set','project.member.remove','channel.member.add','channel.member.remove','environment.authorization.grant','environment.authorization.revoke') AND result_id=subject_id)
    ),
    ADD CONSTRAINT configuration_audit_state CHECK (
        (command_kind IN ('team.create','project.create','channel.create','project.member.set','project.member.remove','channel.member.add','channel.member.remove')
            AND before_state IN ('','viewer','contributor','decider','admin','member')
            AND after_state IN ('','viewer','contributor','decider','admin','member') AND authorization_id IS NULL)
        OR (command_kind IN ('component.create','environment.create') AND before_state='' AND after_state='' AND changed
            AND authorization_id IS NULL AND cardinality(granted_user_ids)=0 AND cardinality(removed_channel_ids)=0 AND cardinality(removed_thread_ids)=0)
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
    DROP CONSTRAINT workspace_configuration_receipts_check,
    ADD CONSTRAINT configuration_receipt_scope CHECK (
        (command_kind IN ('team.create','project.create','component.create','environment.create') AND scope_id=workspace_id AND subject_id='')
        OR (command_kind='channel.create' AND scope_id LIKE 'prj_%' AND subject_id='')
        OR (command_kind IN ('project.member.set','project.member.remove') AND scope_id LIKE 'prj_%' AND subject_id LIKE 'usr_%')
        OR (command_kind IN ('channel.member.add','channel.member.remove') AND scope_id LIKE 'chn_%' AND subject_id LIKE 'usr_%')
        OR (command_kind IN ('environment.authorization.grant','environment.authorization.revoke') AND scope_id LIKE 'env_%' AND subject_id LIKE 'usr_%')
    );

-- Existing event projections are unchanged by the additive v3 contract.
UPDATE radishnexus.activity_items SET projection_version=3 WHERE projection_version=2;

---- create above / drop below ----
-- Forward-only. Preserve authorization generations and immutable Deployment references.
