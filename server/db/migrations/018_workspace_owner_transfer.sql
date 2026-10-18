-- Owner authority is scoped to a membership tenure, never a global account.
ALTER TABLE radishnexus.workspace_memberships
    ADD COLUMN role_revision bigint NOT NULL DEFAULT 1 CHECK (role_revision > 0);

CREATE FUNCTION radishnexus.enforce_workspace_role_revision()
RETURNS trigger LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF NEW.role IS DISTINCT FROM OLD.role THEN
        IF OLD.role_revision=9223372036854775807 OR NEW.role_revision::numeric<>OLD.role_revision::numeric+1 THEN
            RAISE EXCEPTION 'role change requires next revision' USING ERRCODE='23514';
        END IF;
    ELSIF NEW.role_revision<>OLD.role_revision THEN
        RAISE EXCEPTION 'role revision requires role change' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER workspace_role_revision BEFORE UPDATE ON radishnexus.workspace_memberships
FOR EACH ROW EXECUTE FUNCTION radishnexus.enforce_workspace_role_revision();

ALTER TABLE radishnexus.identity_invitations ADD COLUMN creator_role_revision bigint;
UPDATE radishnexus.identity_invitations i SET creator_role_revision=CASE WHEN
    i.consumed_at IS NULL AND i.expires_at>clock_timestamp() AND EXISTS (
        SELECT 1 FROM radishnexus.workspace_memberships m JOIN radishnexus.user_accounts a ON a.user_id=m.user_id
        WHERE m.workspace_id=i.workspace_id AND m.user_id=i.created_by AND m.status='active' AND m.role='owner' AND a.status='active'
    ) THEN 1 ELSE 0 END;
ALTER TABLE radishnexus.identity_invitations ALTER COLUMN creator_role_revision SET NOT NULL;
ALTER TABLE radishnexus.identity_invitations ADD CONSTRAINT invitation_creator_revision CHECK (creator_role_revision>=0);
-- No default: new writers must explicitly bind the current positive revision.

ALTER TABLE radishnexus.workspace_configuration_audit
    ADD COLUMN actor_before_role text,
    ADD COLUMN actor_after_role text,
    ADD COLUMN actor_before_role_revision bigint,
    ADD COLUMN actor_after_role_revision bigint,
    ADD COLUMN subject_before_role_revision bigint,
    ADD COLUMN subject_after_role_revision bigint,
    ADD CONSTRAINT owner_transfer_audit_shape CHECK (
        (command_kind<>'workspace.owner.transfer' AND actor_before_role IS NULL AND actor_after_role IS NULL
            AND actor_before_role_revision IS NULL AND actor_after_role_revision IS NULL
            AND subject_before_role_revision IS NULL AND subject_after_role_revision IS NULL)
        OR (command_kind='workspace.owner.transfer' AND actor_id<>subject_id
            AND actor_before_role IS NOT NULL AND actor_before_role='owner'
            AND actor_after_role IS NOT NULL AND actor_after_role='member'
            AND actor_before_role_revision IS NOT NULL AND actor_before_role_revision>0
            AND subject_before_role_revision IS NOT NULL AND subject_before_role_revision>0
            AND actor_after_role_revision IS NOT NULL AND actor_after_role_revision>0
            AND subject_after_role_revision IS NOT NULL AND subject_after_role_revision>0
            AND actor_after_role_revision::numeric=actor_before_role_revision::numeric+1
            AND subject_after_role_revision::numeric=subject_before_role_revision::numeric+1)
    );

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
        OR (command_kind='workspace.owner.transfer' AND scope_id=workspace_id AND subject_id LIKE 'usr_%')
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
        OR (command_kind='workspace.owner.transfer' AND result_id=subject_id)
        OR (command_kind IN ('project.member.set','project.member.remove','channel.member.add','channel.member.remove','environment.authorization.grant','environment.authorization.revoke') AND result_id=subject_id)
    ),
    ADD CONSTRAINT configuration_audit_state CHECK (
        (command_kind='workspace.owner.transfer' AND before_state='member' AND after_state='owner' AND changed
            AND authorization_id IS NULL AND cardinality(granted_user_ids)=0 AND cardinality(removed_channel_ids)=0 AND cardinality(removed_thread_ids)=0) OR
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
        OR (command_kind='workspace.owner.transfer' AND scope_id=workspace_id AND subject_id LIKE 'usr_%')
    );

CREATE UNIQUE INDEX owner_transfer_operation ON radishnexus.workspace_configuration_receipts(workspace_id,actor_id,client_operation_id)
WHERE command_kind='workspace.owner.transfer';
ALTER TABLE radishnexus.workspace_configuration_receipts ADD CONSTRAINT owner_transfer_operation_format CHECK (
    command_kind<>'workspace.owner.transfer' OR client_operation_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
);

-- Receipt context must name the same immutable transfer, including its actor.
CREATE FUNCTION radishnexus.validate_owner_transfer_receipt()
RETURNS trigger LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF NEW.command_kind='workspace.owner.transfer' AND NOT EXISTS (
        SELECT 1 FROM workspace_configuration_audit a WHERE a.workspace_id=NEW.workspace_id AND a.id=NEW.audit_id
        AND a.command_kind=NEW.command_kind AND a.actor_id=NEW.actor_id AND a.scope_id=NEW.scope_id AND a.subject_id=NEW.subject_id
    ) THEN
        RAISE EXCEPTION 'owner transfer receipt context mismatch' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER owner_transfer_receipt_context BEFORE INSERT ON radishnexus.workspace_configuration_receipts
FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_owner_transfer_receipt();

---- create above / drop below ----
-- Forward-only. Restore a verified matching backup into a new isolated target.
