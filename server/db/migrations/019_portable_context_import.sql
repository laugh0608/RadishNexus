-- Portable history is evidence accepted by the importing owner, not a replay of
-- native commands. Receipts and external delivery state are deliberately absent.
CREATE TABLE radishnexus.context_import_batches (
    operation_id uuid PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces(id),
    actor_id text NOT NULL,
    package_id text NOT NULL CHECK (package_id LIKE 'pkg_%'),
    package_digest text NOT NULL CHECK (package_digest ~ '^[0-9a-f]{64}$'),
    plan_digest text NOT NULL CHECK (plan_digest ~ '^[0-9a-f]{64}$'),
    target_identity text NOT NULL CHECK (target_identity ~ '^[0-9a-f]{64}$'),
    project_id text NOT NULL,
    applied_at timestamptz NOT NULL,
    permission_policy text NOT NULL CHECK (permission_policy='restricted-project-importer-only-v1'),
    UNIQUE(workspace_id,operation_id),
    UNIQUE(workspace_id,package_id),
    UNIQUE(workspace_id,package_digest),
    FOREIGN KEY(workspace_id,actor_id) REFERENCES radishnexus.workspace_memberships(workspace_id,user_id),
    FOREIGN KEY(workspace_id,project_id) REFERENCES radishnexus.projects(workspace_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE radishnexus.context_import_mappings (
    workspace_id text NOT NULL,
    operation_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('team','project','channel','message','thread','decision','ticket','document','actor','entity-link','event')),
    source_id text NOT NULL,
    target_id text NOT NULL,
    source_actor_id text,
    source_visibility text,
    lineage jsonb NOT NULL CHECK (jsonb_typeof(lineage)='array' AND jsonb_array_length(lineage) BETWEEN 1 AND 8),
    PRIMARY KEY(workspace_id,kind,target_id),
    UNIQUE(operation_id,kind,source_id),
    FOREIGN KEY(workspace_id,operation_id) REFERENCES radishnexus.context_import_batches(workspace_id,operation_id)
);
CREATE TABLE radishnexus.context_import_history (
    workspace_id text NOT NULL,
    operation_id uuid NOT NULL,
    event_id text NOT NULL,
    original_type text NOT NULL CHECK (original_type IN ('project.created','channel.created','message.created','thread.started','decision.proposed','decision.accepted','ticket.created','ticket.status-changed','document.created','document.revised')),
    original_actor text NOT NULL,
    original_time timestamptz NOT NULL,
    original_facts jsonb NOT NULL CHECK (jsonb_typeof(original_facts)='object'),
    PRIMARY KEY(workspace_id,event_id),
    FOREIGN KEY(workspace_id,operation_id) REFERENCES radishnexus.context_import_batches(workspace_id,operation_id),
    FOREIGN KEY(workspace_id,event_id) REFERENCES radishnexus.domain_events(workspace_id,event_id) DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY(workspace_id,original_actor) REFERENCES radishnexus.workspace_memberships(workspace_id,user_id)
);
CREATE TABLE radishnexus.context_export_audit (
    package_id text PRIMARY KEY,
    workspace_id text NOT NULL,
    actor_id text NOT NULL,
    project_id text NOT NULL,
    package_digest text NOT NULL CHECK (package_digest ~ '^[0-9a-f]{64}$'),
    occurred_at timestamptz NOT NULL,
    FOREIGN KEY(workspace_id,actor_id) REFERENCES radishnexus.workspace_memberships(workspace_id,user_id),
    FOREIGN KEY(workspace_id,project_id) REFERENCES radishnexus.projects(workspace_id,id)
);
CREATE TRIGGER context_import_batches_immutable BEFORE UPDATE OR DELETE ON radishnexus.context_import_batches
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_collaboration_command_receipt_mutation();
CREATE TRIGGER context_import_mappings_immutable BEFORE UPDATE OR DELETE ON radishnexus.context_import_mappings
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_collaboration_command_receipt_mutation();
CREATE TRIGGER context_import_history_immutable BEFORE UPDATE OR DELETE ON radishnexus.context_import_history
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_collaboration_command_receipt_mutation();
CREATE TRIGGER context_export_audit_immutable BEFORE UPDATE OR DELETE ON radishnexus.context_export_audit
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_collaboration_command_receipt_mutation();

CREATE FUNCTION radishnexus.validate_context_history() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM domain_events e JOIN context_import_batches b ON b.workspace_id=e.workspace_id AND b.operation_id=NEW.operation_id
        JOIN context_import_mappings m ON m.workspace_id=b.workspace_id AND m.operation_id=b.operation_id AND m.kind='event' AND m.target_id=e.event_id
        JOIN context_import_mappings a ON a.workspace_id=b.workspace_id AND a.operation_id=b.operation_id AND a.kind='actor' AND a.target_id=NEW.original_actor
        JOIN context_import_mappings o ON o.workspace_id=b.workspace_id AND o.operation_id=b.operation_id AND o.kind=e.primary_entity_type AND o.target_id=e.primary_entity_id
        WHERE e.workspace_id=NEW.workspace_id AND e.event_id=NEW.event_id AND e.event_type='context.history-recorded'
        AND e.schema_version=1 AND e.actor_kind='user' AND e.actor_id=b.actor_id AND e.source_kind='import'
        AND e.source_id=b.operation_id::text AND e.project_id=b.project_id AND e.occurred_at=b.applied_at AND e.payload='{}'::jsonb
        AND e.primary_entity_type=split_part(NEW.original_type,'.',1)
    ) THEN RAISE EXCEPTION 'Invalid imported history provenance' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER context_history_source AFTER INSERT ON radishnexus.context_import_history
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_context_history();
CREATE FUNCTION radishnexus.validate_context_event() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF NEW.event_type='context.history-recorded' AND NOT EXISTS (
        SELECT 1 FROM context_import_history WHERE workspace_id=NEW.workspace_id AND event_id=NEW.event_id
    ) THEN RAISE EXCEPTION 'Imported event requires history evidence' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER context_event_history AFTER INSERT ON radishnexus.domain_events
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_context_event();

-- A historical identity cannot be made into a login or an active membership by
-- normal account management. Claiming authorship needs its own future contract.
CREATE FUNCTION radishnexus.guard_context_actor() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM context_import_mappings WHERE kind='actor' AND target_id=NEW.user_id) THEN
        IF TG_TABLE_NAME NOT IN ('workspace_memberships','user_accounts')
           OR (TG_TABLE_NAME='workspace_memberships' AND to_jsonb(NEW)->>'status'<>'suspended')
           OR (TG_TABLE_NAME='user_accounts' AND to_jsonb(NEW)->>'status'<>'disabled') THEN
            RAISE EXCEPTION 'Historical identities cannot authenticate or become active' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER context_actor_account BEFORE INSERT OR UPDATE ON radishnexus.user_accounts
FOR EACH ROW EXECUTE FUNCTION radishnexus.guard_context_actor();
CREATE TRIGGER context_actor_membership BEFORE INSERT OR UPDATE ON radishnexus.workspace_memberships
FOR EACH ROW EXECUTE FUNCTION radishnexus.guard_context_actor();
CREATE TRIGGER context_actor_credentials BEFORE INSERT OR UPDATE ON radishnexus.local_credentials
FOR EACH ROW EXECUTE FUNCTION radishnexus.guard_context_actor();
CREATE TRIGGER context_actor_sessions BEFORE INSERT OR UPDATE ON radishnexus.user_sessions
FOR EACH ROW EXECUTE FUNCTION radishnexus.guard_context_actor();
CREATE TRIGGER context_actor_external_identity BEFORE INSERT OR UPDATE ON radishnexus.external_identities
FOR EACH ROW EXECUTE FUNCTION radishnexus.guard_context_actor();

CREATE OR REPLACE FUNCTION radishnexus.validate_document_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE current_number integer;
BEGIN
    SELECT current_revision INTO current_number FROM documents WHERE workspace_id=NEW.workspace_id AND id=NEW.document_id;
    IF NEW.revision>current_number OR (NEW.revision>1 AND NOT EXISTS (
        SELECT 1 FROM document_revisions WHERE workspace_id=NEW.workspace_id AND document_id=NEW.document_id AND revision=NEW.revision-1
    )) OR NOT EXISTS (
        SELECT 1 FROM domain_events e LEFT JOIN context_import_history h ON h.workspace_id=e.workspace_id AND h.event_id=e.event_id
        WHERE e.workspace_id=NEW.workspace_id AND e.event_id=NEW.source_event_id AND e.primary_entity_type='document'
        AND e.primary_entity_id=NEW.document_id AND (
            (e.actor_kind='user' AND e.actor_id=NEW.created_by
             AND e.event_type=CASE WHEN NEW.revision=1 THEN 'document.created' ELSE 'document.revised' END
             AND e.payload->>'revision'=NEW.revision::text)
            OR (e.event_type='context.history-recorded' AND e.source_kind='import'
                AND h.original_actor=NEW.created_by AND h.original_time=NEW.created_at
                AND h.original_type=CASE WHEN NEW.revision=1 THEN 'document.created' ELSE 'document.revised' END
                AND h.original_facts->>'revision'=NEW.revision::text
                AND (h.original_facts->>'restored_from_revision')::integer IS NOT DISTINCT FROM NEW.restored_from_revision)
        )
    ) THEN RAISE EXCEPTION 'Invalid Document revision source or progression' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;
CREATE OR REPLACE FUNCTION radishnexus.validate_document_relation() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF NEW.from_type='ticket' AND NEW.to_type='document' AND NEW.relation_type='relates-to' AND NOT EXISTS (
        SELECT 1 FROM tickets t JOIN documents d ON d.workspace_id=t.workspace_id AND d.governing_project_id=t.governing_project_id
        JOIN document_revisions r ON r.workspace_id=d.workspace_id AND r.document_id=d.id AND r.revision=1
        LEFT JOIN context_import_history h ON h.workspace_id=r.workspace_id AND h.event_id=r.source_event_id
        WHERE t.workspace_id=NEW.workspace_id AND t.id=NEW.from_id AND d.id=NEW.to_id
        AND NEW.assertion='asserted' AND NEW.source_event_id=r.source_event_id AND NEW.metadata='{}'::jsonb AND (
            (NEW.origin='user' AND NEW.created_by_kind='user' AND NEW.created_by_id=r.created_by AND NEW.origin_ref IS NULL)
            OR (NEW.origin='import' AND NEW.created_by_kind='import' AND NEW.created_by_id=h.operation_id::text
                AND NEW.origin_ref=h.operation_id::text AND h.original_actor=r.created_by
                AND h.original_type='document.created' AND h.original_facts->'ticket'=jsonb_build_object('type','ticket','id',t.id))
        )
    ) THEN RAISE EXCEPTION 'Invalid Document relation scope or provenance' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;
UPDATE radishnexus.activity_items SET projection_version=8 WHERE projection_version=7;

CREATE FUNCTION radishnexus.validate_context_mapping() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE part jsonb;
BEGIN
    FOR part IN SELECT value FROM jsonb_array_elements(NEW.lineage) LOOP
        IF jsonb_typeof(part)<>'object' OR part<>jsonb_build_object('package_id',part->>'package_id','package_digest',part->>'package_digest','source_id',part->>'source_id')
           OR COALESCE(part->>'package_id','') !~ '^pkg_[A-Za-z0-9_-]+$'
           OR COALESCE(part->>'package_digest','') !~ '^[0-9a-f]{64}$'
           OR COALESCE(part->>'source_id','') !~ '^[A-Za-z0-9_-]{1,128}$' THEN
            RAISE EXCEPTION 'Invalid context lineage' USING ERRCODE='23514';
        END IF;
    END LOOP;
    IF NEW.kind='actor' AND (NOT EXISTS (
        SELECT 1 FROM user_accounts a JOIN workspace_memberships m ON m.user_id=a.user_id
        WHERE a.user_id=NEW.target_id AND a.status='disabled' AND m.workspace_id=NEW.workspace_id AND m.status='suspended' AND m.role='member'
    ) OR EXISTS(SELECT 1 FROM local_credentials WHERE user_id=NEW.target_id)
      OR EXISTS(SELECT 1 FROM external_identities WHERE user_id=NEW.target_id)
      OR EXISTS(SELECT 1 FROM user_sessions WHERE user_id=NEW.target_id)
      OR EXISTS(SELECT 1 FROM workspace_memberships WHERE user_id=NEW.target_id AND status='active')) THEN
        RAISE EXCEPTION 'Invalid historical identity' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER context_mapping_source AFTER INSERT ON radishnexus.context_import_mappings
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_context_mapping();

CREATE FUNCTION radishnexus.validate_context_facts() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE f jsonb:=NEW.original_facts; expected jsonb; pair record; expected_kind text;
BEGIN
    CASE NEW.original_type
      WHEN 'project.created','channel.created' THEN expected:=jsonb_build_object('status','active');
      WHEN 'message.created' THEN
        expected:=jsonb_build_object('channel',f->'channel');
        IF f ? 'thread' THEN expected:=expected||jsonb_build_object('thread',f->'thread'); END IF;
      WHEN 'thread.started' THEN expected:=jsonb_build_object('channel',f->'channel','message',f->'message');
      WHEN 'decision.proposed' THEN expected:=jsonb_build_object('status','proposed','evidence',f->'evidence');
      WHEN 'decision.accepted' THEN expected:=jsonb_build_object('status','accepted');
      WHEN 'ticket.created' THEN expected:=jsonb_build_object('status','open','decision',f->'decision');
      WHEN 'ticket.status-changed' THEN
        IF NOT valid_ticket_status_transition(f->>'from_status',f->>'to_status')
           OR COALESCE((f->>'from_status_revision')::integer,0)<1
           OR (f->>'to_status_revision')::integer IS DISTINCT FROM (f->>'from_status_revision')::integer+1 THEN
            RAISE EXCEPTION 'Invalid historical Ticket transition' USING ERRCODE='23514';
        END IF;
        expected:=jsonb_build_object('from_status',f->>'from_status','to_status',f->>'to_status',
            'from_status_revision',(f->>'from_status_revision')::integer,'to_status_revision',(f->>'to_status_revision')::integer);
      WHEN 'document.created' THEN expected:=jsonb_build_object('revision',1,'ticket',f->'ticket');
      WHEN 'document.revised' THEN
        IF COALESCE((f->>'revision')::integer,0)<2 THEN RAISE EXCEPTION 'Invalid historical revision' USING ERRCODE='23514'; END IF;
        expected:=jsonb_build_object('revision',(f->>'revision')::integer);
        IF f ? 'restored_from_revision' THEN
            IF COALESCE((f->>'restored_from_revision')::integer,0)<1 OR (f->>'restored_from_revision')::integer>=(f->>'revision')::integer THEN
                RAISE EXCEPTION 'Invalid historical restoration' USING ERRCODE='23514';
            END IF;
            expected:=expected||jsonb_build_object('restored_from_revision',(f->>'restored_from_revision')::integer);
        END IF;
    END CASE;
    IF f IS DISTINCT FROM expected THEN RAISE EXCEPTION 'Invalid historical fact fields' USING ERRCODE='23514'; END IF;
    FOR pair IN SELECT key,value FROM jsonb_each(f) WHERE key IN ('channel','thread','message','evidence','decision','ticket') LOOP
        expected_kind:=CASE WHEN pair.key='evidence' THEN 'thread' ELSE pair.key END;
        IF pair.value IS DISTINCT FROM jsonb_build_object('type',expected_kind,'id',pair.value->>'id') OR NOT EXISTS (
            SELECT 1 FROM context_import_mappings WHERE workspace_id=NEW.workspace_id AND operation_id=NEW.operation_id
            AND kind=expected_kind AND target_id=pair.value->>'id'
        ) THEN RAISE EXCEPTION 'Invalid historical reference' USING ERRCODE='23514'; END IF;
    END LOOP;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER context_history_facts AFTER INSERT ON radishnexus.context_import_history
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_context_facts();
CREATE FUNCTION radishnexus.validate_context_link() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF NEW.origin='import' AND NOT EXISTS (
        SELECT 1 FROM context_import_mappings m JOIN context_import_history h
        ON h.workspace_id=m.workspace_id AND h.operation_id=m.operation_id AND h.event_id=NEW.source_event_id
        JOIN domain_events e ON e.workspace_id=h.workspace_id AND e.event_id=h.event_id
        WHERE m.workspace_id=NEW.workspace_id AND m.kind='entity-link' AND m.target_id=NEW.id
        AND NEW.created_by_kind='import' AND NEW.created_by_id=m.operation_id::text AND NEW.origin_ref=m.operation_id::text
        AND m.source_actor_id=h.original_actor AND NEW.created_at=h.original_time AND NEW.assertion='asserted' AND NEW.metadata='{}'
        AND (
            (NEW.from_type='thread' AND NEW.relation_type='started-from' AND NEW.to_type='message' AND h.original_type='thread.started'
                AND e.primary_entity_type='thread' AND e.primary_entity_id=NEW.from_id AND h.original_facts->'message'=jsonb_build_object('type','message','id',NEW.to_id))
            OR (NEW.from_type='decision' AND NEW.relation_type='derived-from' AND NEW.to_type='thread' AND h.original_type='decision.proposed'
                AND e.primary_entity_type='decision' AND e.primary_entity_id=NEW.from_id AND h.original_facts->'evidence'=jsonb_build_object('type','thread','id',NEW.to_id))
            OR (NEW.from_type='ticket' AND NEW.relation_type='implements' AND NEW.to_type='decision' AND h.original_type='ticket.created'
                AND e.primary_entity_type='ticket' AND e.primary_entity_id=NEW.from_id AND h.original_facts->'decision'=jsonb_build_object('type','decision','id',NEW.to_id))
            OR (NEW.from_type='ticket' AND NEW.relation_type='relates-to' AND NEW.to_type='document' AND h.original_type='document.created'
                AND e.primary_entity_type='document' AND e.primary_entity_id=NEW.to_id AND h.original_facts->'ticket'=jsonb_build_object('type','ticket','id',NEW.from_id))
        )
    ) THEN RAISE EXCEPTION 'Imported link requires exact provenance' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER context_link_source AFTER INSERT ON radishnexus.entity_links
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_context_link();
CREATE FUNCTION radishnexus.prevent_context_delivery() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM domain_events WHERE event_id=NEW.event_id AND event_type='context.history-recorded') THEN
        RAISE EXCEPTION 'Imported history cannot be enqueued for delivery' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER context_history_no_delivery BEFORE INSERT ON radishnexus.outbox_deliveries
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_context_delivery();
