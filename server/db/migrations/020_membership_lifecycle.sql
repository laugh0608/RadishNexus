-- Workspace access, collaboration management and historical identity remain
-- separate. Removing a membership never deletes authors or their provenance.
ALTER TABLE radishnexus.workspace_memberships
 DROP CONSTRAINT workspace_memberships_status,
 ADD CONSTRAINT workspace_memberships_status CHECK(status IN ('active','suspended','removed')),
 ADD COLUMN lifecycle_revision bigint NOT NULL DEFAULT 1 CHECK(lifecycle_revision>0),
 ADD COLUMN session_valid_after timestamptz NOT NULL DEFAULT '-infinity',
 ADD CONSTRAINT workspace_removed_is_member CHECK(status<>'removed' OR role='member');

CREATE FUNCTION radishnexus.advance_membership_lifecycle() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
 IF NEW.status IS DISTINCT FROM OLD.status THEN
   IF OLD.lifecycle_revision=9223372036854775807 THEN RAISE EXCEPTION 'Membership revision exhausted' USING ERRCODE='23514'; END IF;
   IF NEW.lifecycle_revision<>OLD.lifecycle_revision THEN RAISE EXCEPTION 'Membership revision is database assigned' USING ERRCODE='23514'; END IF;
   NEW.lifecycle_revision=OLD.lifecycle_revision+1;
   NEW.session_valid_after=greatest(OLD.session_valid_after,clock_timestamp());
 ELSIF NEW.lifecycle_revision<>OLD.lifecycle_revision OR NEW.session_valid_after<>OLD.session_valid_after THEN
   RAISE EXCEPTION 'Membership admission requires a state transition' USING ERRCODE='23514';
 END IF;
 IF NEW.status='removed' AND NEW.role<>'member' THEN RAISE EXCEPTION 'Owner must hand off before removal' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER membership_lifecycle BEFORE UPDATE ON radishnexus.workspace_memberships
FOR EACH ROW EXECUTE FUNCTION radishnexus.advance_membership_lifecycle();

ALTER TABLE radishnexus.projects ADD COLUMN management_revision bigint NOT NULL DEFAULT 1 CHECK(management_revision>0);
ALTER TABLE radishnexus.channels ADD COLUMN management_revision bigint NOT NULL DEFAULT 1 CHECK(management_revision>0);
ALTER TABLE radishnexus.threads ADD COLUMN management_revision bigint NOT NULL DEFAULT 1 CHECK(management_revision>0);
ALTER TABLE radishnexus.channel_memberships ADD COLUMN management_role text NOT NULL DEFAULT 'member' CHECK(management_role IN ('member','manager'));
ALTER TABLE radishnexus.thread_memberships ADD COLUMN management_role text NOT NULL DEFAULT 'member' CHECK(management_role IN ('member','manager'));

-- Only a still-authorized creator receives the new local responsibility.
UPDATE radishnexus.thread_memberships m SET management_role='manager'
FROM radishnexus.threads t,radishnexus.workspace_memberships w,radishnexus.user_accounts a
WHERE m.workspace_id=t.workspace_id AND m.thread_id=t.id AND m.user_id=t.created_by
 AND w.workspace_id=m.workspace_id AND w.user_id=m.user_id AND w.status='active'
 AND a.user_id=m.user_id AND a.status='active' AND t.visibility='restricted'
 AND EXISTS(SELECT 1 FROM radishnexus.project_memberships p WHERE p.workspace_id=t.workspace_id AND p.project_id=t.governing_project_id AND p.user_id=m.user_id)
 AND (t.origin_channel_id IS NULL OR EXISTS(SELECT 1 FROM radishnexus.channels c WHERE c.workspace_id=t.workspace_id AND c.id=t.origin_channel_id AND (c.visibility='project' OR EXISTS(SELECT 1 FROM radishnexus.channel_memberships cm WHERE cm.workspace_id=c.workspace_id AND cm.channel_id=c.id AND cm.user_id=m.user_id))));

-- Imported historical creators must never acquire management rights. A still
-- explicitly authorized importer may hold responsibility in the target.
UPDATE radishnexus.channel_memberships m SET management_role='manager'
FROM radishnexus.context_import_mappings im,radishnexus.context_import_batches b,radishnexus.channels c
WHERE im.workspace_id=m.workspace_id AND im.kind='channel' AND im.target_id=m.channel_id
 AND b.workspace_id=im.workspace_id AND b.operation_id=im.operation_id AND b.actor_id=m.user_id
 AND c.workspace_id=m.workspace_id AND c.id=m.channel_id
 AND EXISTS(SELECT 1 FROM radishnexus.project_memberships p JOIN radishnexus.workspace_memberships w USING(workspace_id,user_id) JOIN radishnexus.user_accounts a ON a.user_id=p.user_id WHERE p.workspace_id=m.workspace_id AND p.project_id=c.governing_project_id AND p.user_id=m.user_id AND p.role='admin' AND w.status='active' AND a.status='active');
UPDATE radishnexus.thread_memberships m SET management_role='manager'
FROM radishnexus.context_import_mappings im,radishnexus.context_import_batches b,radishnexus.threads t
WHERE im.workspace_id=m.workspace_id AND im.kind='thread' AND im.target_id=m.thread_id
 AND b.workspace_id=im.workspace_id AND b.operation_id=im.operation_id AND b.actor_id=m.user_id
 AND t.workspace_id=m.workspace_id AND t.id=m.thread_id
 AND EXISTS(SELECT 1 FROM radishnexus.project_memberships p JOIN radishnexus.workspace_memberships w USING(workspace_id,user_id) JOIN radishnexus.user_accounts a ON a.user_id=p.user_id WHERE p.workspace_id=m.workspace_id AND p.project_id=t.governing_project_id AND p.user_id=m.user_id AND p.role='admin' AND w.status='active' AND a.status='active')
 AND (t.origin_channel_id IS NULL OR EXISTS(SELECT 1 FROM radishnexus.channels c WHERE c.workspace_id=t.workspace_id AND c.id=t.origin_channel_id AND (c.visibility='project' OR EXISTS(SELECT 1 FROM radishnexus.channel_memberships cm WHERE cm.workspace_id=c.workspace_id AND cm.channel_id=c.id AND cm.user_id=m.user_id))));

-- Each scope has its own generation. A hidden Thread change never advances
-- a public Project generation; deleting/recreating a grant cannot cause ABA.
CREATE FUNCTION radishnexus.advance_management_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE item record;
BEGIN
 IF TG_OP='UPDATE' AND to_jsonb(NEW)=to_jsonb(OLD) THEN RETURN NEW; END IF;
 IF TG_OP='DELETE' THEN item=OLD; ELSE item=NEW; END IF;
 IF TG_TABLE_NAME='project_memberships' THEN
   UPDATE projects SET management_revision=management_revision+1 WHERE workspace_id=item.workspace_id AND id=item.project_id;
 ELSIF TG_TABLE_NAME='channel_memberships' THEN
   UPDATE channels SET management_revision=management_revision+1 WHERE workspace_id=item.workspace_id AND id=item.channel_id;
 ELSE
   UPDATE threads SET management_revision=management_revision+1 WHERE workspace_id=item.workspace_id AND id=item.thread_id;
 END IF;
 RETURN NULL;
END;
$$;
CREATE TRIGGER project_management_revision AFTER INSERT OR UPDATE OR DELETE ON radishnexus.project_memberships FOR EACH ROW EXECUTE FUNCTION radishnexus.advance_management_revision();
CREATE TRIGGER channel_management_revision AFTER INSERT OR UPDATE OR DELETE ON radishnexus.channel_memberships FOR EACH ROW EXECUTE FUNCTION radishnexus.advance_management_revision();
CREATE TRIGGER thread_management_revision AFTER INSERT OR UPDATE OR DELETE ON radishnexus.thread_memberships FOR EACH ROW EXECUTE FUNCTION radishnexus.advance_management_revision();

ALTER TABLE radishnexus.identity_invitations
 ADD COLUMN creator_lifecycle_revision bigint NOT NULL DEFAULT 0 CHECK(creator_lifecycle_revision>=0),
 ADD COLUMN target_user_id text REFERENCES radishnexus.users(id),
 ADD COLUMN target_lifecycle_revision bigint,
 ADD CONSTRAINT invitation_readmission_target CHECK((target_user_id IS NULL AND target_lifecycle_revision IS NULL) OR (target_user_id IS NOT NULL AND target_lifecycle_revision IS NOT NULL AND target_lifecycle_revision>0));
UPDATE radishnexus.identity_invitations i SET creator_lifecycle_revision=m.lifecycle_revision
FROM radishnexus.workspace_memberships m,radishnexus.user_accounts a
WHERE m.workspace_id=i.workspace_id AND m.user_id=i.created_by AND m.status='active' AND m.role='owner'
 AND a.user_id=m.user_id AND a.status='active' AND i.creator_role_revision=m.role_revision AND i.consumed_at IS NULL AND i.expires_at>clock_timestamp();
ALTER TABLE radishnexus.identity_invitations ALTER COLUMN creator_lifecycle_revision DROP DEFAULT;

CREATE FUNCTION radishnexus.management_command(kind text, scope text, workspace text, subject text) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$ SELECT subject LIKE 'usr_%' AND (
 (kind IN ('management.workspace.suspend','management.workspace.resume','management.workspace.remove','management.workspace.leave','management.workspace.reinvite') AND scope=workspace)
 OR (kind IN ('management.project.add','management.project.role','management.project.remove','management.project.handoff','management.project.leave') AND scope LIKE 'prj_%')
 OR (kind IN ('management.channel.add','management.channel.role','management.channel.remove','management.channel.handoff','management.channel.leave') AND scope LIKE 'chn_%')
 OR (kind IN ('management.thread.add','management.thread.role','management.thread.remove','management.thread.handoff','management.thread.leave') AND scope LIKE 'thr_%')
) $$;
ALTER TABLE radishnexus.workspace_configuration_audit
 ADD COLUMN management_plan_digest text,
 ADD COLUMN management_before_revision bigint,
 ADD COLUMN management_after_revision bigint,
 ADD COLUMN management_actor_after_role text,
 ADD COLUMN revoked_authorization_ids text[] NOT NULL DEFAULT '{}',
 ADD CONSTRAINT management_audit_shape CHECK (
 (command_kind NOT LIKE 'management.%' AND management_plan_digest IS NULL AND management_before_revision IS NULL AND management_after_revision IS NULL AND management_actor_after_role IS NULL AND cardinality(revoked_authorization_ids)=0)
 OR (radishnexus.management_command(command_kind,scope_id,workspace_id,subject_id)
  AND management_plan_digest IS NOT NULL AND management_plan_digest ~ '^[0-9a-f]{64}$'
  AND management_before_revision IS NOT NULL AND management_before_revision>0
  AND management_after_revision IS NOT NULL AND management_after_revision>=management_before_revision
  AND management_actor_after_role IS NOT NULL AND management_actor_after_role IN ('','viewer','contributor','decider','member')
  AND (command_kind LIKE '%.handoff' OR management_actor_after_role='')
  AND (command_kind NOT LIKE 'management.workspace.%' OR
    (command_kind='management.workspace.suspend' AND before_state='active' AND after_state='suspended') OR
    (command_kind='management.workspace.resume' AND before_state='suspended' AND after_state='active') OR
    (command_kind IN ('management.workspace.remove','management.workspace.leave') AND before_state IN ('active','suspended') AND after_state='removed') OR
    (command_kind='management.workspace.reinvite' AND before_state='removed' AND after_state='removed'))
  AND (command_kind NOT LIKE 'management.project.%' OR
    (command_kind='management.project.add' AND before_state='' AND after_state IN ('viewer','contributor','decider')) OR
    (command_kind IN ('management.project.role','management.project.handoff') AND before_state IN ('viewer','contributor','decider','admin') AND after_state IN ('viewer','contributor','decider','admin')) OR
    (command_kind IN ('management.project.remove','management.project.leave') AND before_state IN ('viewer','contributor','decider','admin') AND after_state=''))
  AND (command_kind NOT LIKE 'management.channel.%' AND command_kind NOT LIKE 'management.thread.%' OR
    (command_kind IN ('management.channel.add','management.thread.add') AND before_state='' AND after_state='member') OR
    (command_kind IN ('management.channel.role','management.channel.handoff','management.thread.role','management.thread.handoff') AND before_state IN ('member','manager') AND after_state IN ('member','manager')) OR
    (command_kind IN ('management.channel.remove','management.channel.leave','management.thread.remove','management.thread.leave') AND before_state IN ('member','manager') AND after_state=''))
  AND (command_kind NOT LIKE '%.handoff' OR after_state=CASE WHEN command_kind='management.project.handoff' THEN 'admin' ELSE 'manager' END)
  AND (command_kind NOT LIKE '%.leave' OR actor_id=subject_id)
  AND (command_kind IN ('management.workspace.suspend','management.workspace.remove','management.workspace.leave') OR cardinality(revoked_authorization_ids)=0))
 );

-- Preserve every previous constraint verbatim as the old-command branch.
-- The added branch is limited to the closed set above, never arbitrary JSON.
DO $$
DECLARE constraint_name text; original text; extension text; table_name text;
BEGIN
 FOREACH constraint_name IN ARRAY ARRAY['configuration_audit_scope','configuration_audit_result','configuration_audit_state','configuration_receipt_scope'] LOOP
   table_name=CASE WHEN constraint_name='configuration_receipt_scope' THEN 'workspace_configuration_receipts' ELSE 'workspace_configuration_audit' END;
   SELECT pg_get_constraintdef(c.oid) INTO STRICT original FROM pg_constraint c WHERE c.conrelid=('radishnexus.'||table_name)::regclass AND c.conname=constraint_name;
   IF left(original,7)<>'CHECK (' OR right(original,1)<>')' THEN RAISE EXCEPTION 'Unexpected configuration constraint'; END IF;
   original=substring(original FROM 8 FOR length(original)-8);
   extension='radishnexus.management_command(command_kind,scope_id,workspace_id,subject_id)';
   IF constraint_name='configuration_audit_result' THEN extension=extension||' AND result_id=subject_id'; END IF;
   IF constraint_name='configuration_audit_state' THEN extension=extension||' AND before_state IN ('''',''active'',''suspended'',''removed'',''viewer'',''contributor'',''decider'',''admin'',''member'',''manager'') AND after_state IN ('''',''active'',''suspended'',''removed'',''viewer'',''contributor'',''decider'',''admin'',''member'',''manager'') AND authorization_id IS NULL'; END IF;
   EXECUTE format('ALTER TABLE radishnexus.%I DROP CONSTRAINT %I, ADD CONSTRAINT %I CHECK ((%s) OR (%s))',table_name,constraint_name,constraint_name,original,extension);
 END LOOP;
END;
$$;
CREATE UNIQUE INDEX management_operation ON radishnexus.workspace_configuration_receipts(workspace_id,actor_id,client_operation_id) WHERE command_kind LIKE 'management.%';
CREATE FUNCTION radishnexus.validate_management_receipt() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
 IF NEW.command_kind LIKE 'management.%' AND NOT EXISTS(SELECT 1 FROM workspace_configuration_audit a WHERE a.workspace_id=NEW.workspace_id AND a.id=NEW.audit_id AND a.actor_id=NEW.actor_id AND a.command_kind=NEW.command_kind AND a.scope_id=NEW.scope_id AND a.subject_id=NEW.subject_id AND a.occurred_at=NEW.created_at) THEN
  RAISE EXCEPTION 'Management receipt context mismatch' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER management_receipt_context BEFORE INSERT ON radishnexus.workspace_configuration_receipts FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_management_receipt();
CREATE INDEX management_audit_scope ON radishnexus.workspace_configuration_audit(workspace_id,scope_id,id) WHERE command_kind LIKE 'management.%';

---- create above / drop below ----
-- Forward-only; restore a matching pre-upgrade backup into a new target.
