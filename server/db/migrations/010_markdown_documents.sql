INSERT INTO radishnexus.entity_types(type_name,id_prefix) VALUES ('document','doc_');
INSERT INTO radishnexus.relation_types(from_type,relation_type,to_type) VALUES ('ticket','relates-to','document');

CREATE TABLE radishnexus.documents (
    id text PRIMARY KEY CHECK (radishnexus.valid_entity_id('document',id)),
    workspace_id text NOT NULL,
    governing_project_id text NOT NULL,
    visibility text NOT NULL DEFAULT 'project' CHECK (visibility = 'project'),
    current_revision integer NOT NULL CHECK (current_revision > 0),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE(workspace_id,id),
    FOREIGN KEY(workspace_id,governing_project_id) REFERENCES radishnexus.projects(workspace_id,id),
    FOREIGN KEY(workspace_id,created_by) REFERENCES radishnexus.workspace_memberships(workspace_id,user_id)
);
CREATE INDEX documents_project_page ON radishnexus.documents(workspace_id,governing_project_id,id);

CREATE TABLE radishnexus.document_revisions (
    workspace_id text NOT NULL,
    document_id text NOT NULL,
    revision integer NOT NULL CHECK (revision > 0),
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200 AND title !~ '[[:cntrl:]]'),
    body_markdown text NOT NULL CHECK (octet_length(body_markdown) <= 262144 AND position(chr(13) in body_markdown) = 0),
    format_version text NOT NULL CHECK (format_version = 'nexus-markdown-v1'),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL,
    source_event_id text NOT NULL,
    restored_from_revision integer CHECK (restored_from_revision > 0 AND restored_from_revision < revision),
    PRIMARY KEY(workspace_id,document_id,revision),
    FOREIGN KEY(workspace_id,document_id) REFERENCES radishnexus.documents(workspace_id,id) DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY(workspace_id,created_by) REFERENCES radishnexus.workspace_memberships(workspace_id,user_id),
    FOREIGN KEY(workspace_id,source_event_id) REFERENCES radishnexus.domain_events(workspace_id,event_id) DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY(workspace_id,document_id,restored_from_revision) REFERENCES radishnexus.document_revisions(workspace_id,document_id,revision) DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE radishnexus.documents ADD CONSTRAINT documents_current_revision
    FOREIGN KEY(workspace_id,id,current_revision) REFERENCES radishnexus.document_revisions(workspace_id,document_id,revision) DEFERRABLE INITIALLY DEFERRED;

CREATE FUNCTION radishnexus.enforce_document_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'Documents cannot be deleted' USING ERRCODE='23514'; END IF;
    IF ROW(NEW.id,NEW.workspace_id,NEW.governing_project_id,NEW.visibility,NEW.created_by,NEW.created_at)
       IS DISTINCT FROM ROW(OLD.id,OLD.workspace_id,OLD.governing_project_id,OLD.visibility,OLD.created_by,OLD.created_at)
       OR NEW.current_revision::bigint <> OLD.current_revision::bigint + 1 THEN
        RAISE EXCEPTION 'Document identity and revision progression are immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER documents_identity BEFORE UPDATE OR DELETE ON radishnexus.documents
FOR EACH ROW EXECUTE FUNCTION radishnexus.enforce_document_identity();
CREATE TRIGGER document_revisions_immutable BEFORE UPDATE OR DELETE ON radishnexus.document_revisions
FOR EACH ROW EXECUTE FUNCTION radishnexus.prevent_collaboration_command_receipt_mutation();

CREATE FUNCTION radishnexus.validate_document_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE current_number integer;
BEGIN
    SELECT current_revision INTO current_number FROM documents
    WHERE workspace_id=NEW.workspace_id AND id=NEW.document_id;
    IF NEW.revision>current_number OR (NEW.revision>1 AND NOT EXISTS (
        SELECT 1 FROM document_revisions WHERE workspace_id=NEW.workspace_id
        AND document_id=NEW.document_id AND revision=NEW.revision-1
    )) OR NOT EXISTS (
        SELECT 1 FROM domain_events WHERE workspace_id=NEW.workspace_id
        AND event_id=NEW.source_event_id AND primary_entity_type='document'
        AND primary_entity_id=NEW.document_id AND actor_kind='user' AND actor_id=NEW.created_by
        AND event_type=CASE WHEN NEW.revision=1 THEN 'document.created' ELSE 'document.revised' END
        AND payload->>'revision'=NEW.revision::text
    ) THEN RAISE EXCEPTION 'Invalid Document revision source or progression' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER document_revision_source AFTER INSERT ON radishnexus.document_revisions
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_document_revision();

CREATE FUNCTION radishnexus.validate_document_relation() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF NEW.from_type='ticket' AND NEW.to_type='document' AND NEW.relation_type='relates-to' AND NOT EXISTS (
        SELECT 1 FROM tickets t JOIN documents d ON d.workspace_id=t.workspace_id
        AND d.governing_project_id=t.governing_project_id
        JOIN document_revisions r ON r.workspace_id=d.workspace_id AND r.document_id=d.id AND r.revision=1
        WHERE t.workspace_id=NEW.workspace_id AND t.id=NEW.from_id AND d.id=NEW.to_id
        AND NEW.assertion='asserted' AND NEW.origin='user' AND NEW.created_by_kind='user'
        AND NEW.created_by_id=r.created_by AND NEW.source_event_id=r.source_event_id
        AND NEW.origin_ref IS NULL AND NEW.metadata='{}'::jsonb
    ) THEN RAISE EXCEPTION 'Invalid Document relation scope or provenance' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER document_relation_scope BEFORE INSERT ON radishnexus.entity_links
FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_document_relation();

ALTER TABLE radishnexus.collaboration_command_receipts
    ADD COLUMN result_revision integer,
    DROP CONSTRAINT collaboration_command_receipts_kind,
    DROP CONSTRAINT collaboration_command_receipts_target,
    DROP CONSTRAINT collaboration_command_receipts_result,
    ADD CONSTRAINT collaboration_command_receipts_kind CHECK (command_kind IN ('decision.propose','decision.accept','ticket.create','document.create','document.save','document.restore')),
    ADD CONSTRAINT collaboration_command_receipts_target CHECK (
        (command_kind='decision.propose' AND target_type='thread') OR
        (command_kind IN ('decision.accept','ticket.create') AND target_type='decision') OR
        (command_kind='document.create' AND target_type='ticket') OR
        (command_kind IN ('document.save','document.restore') AND target_type='document')),
    ADD CONSTRAINT collaboration_command_receipts_result CHECK (
        (command_kind IN ('decision.propose','decision.accept') AND result_type='decision' AND result_revision IS NULL) OR
        (command_kind='ticket.create' AND result_type='ticket' AND result_revision IS NULL) OR
        (command_kind IN ('document.create','document.save','document.restore') AND result_type='document' AND result_revision IS NOT NULL AND result_revision>0));

-- Activity is rebuildable. Preserve existing rows under the new projector contract.
UPDATE radishnexus.activity_items SET projection_version=2 WHERE projection_version=1;

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
        WHEN 'document' THEN
            SELECT workspace_id INTO resolved_workspace FROM documents WHERE id = entity_id;
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
