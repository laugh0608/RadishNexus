INSERT INTO radishnexus.entity_types (type_name,id_prefix) VALUES ('ticket-label','lbl_');

ALTER TABLE radishnexus.tickets
    ADD COLUMN ticket_type text NOT NULL DEFAULT 'unset' CHECK (ticket_type IN ('unset','requirement','task','defect')),
    ADD COLUMN classification_revision integer NOT NULL DEFAULT 1 CHECK (classification_revision > 0),
    ADD UNIQUE (workspace_id,governing_project_id,id);

CREATE TABLE radishnexus.ticket_labels (
    id text PRIMARY KEY CHECK (radishnexus.valid_entity_id('ticket-label',id)),
    workspace_id text NOT NULL,
    project_id text NOT NULL,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 40 AND name !~ '[[:cntrl:]]'),
    name_key text COLLATE "C" GENERATED ALWAYS AS (translate(name,'ABCDEFGHIJKLMNOPQRSTUVWXYZ','abcdefghijklmnopqrstuvwxyz')) STORED,
    status text NOT NULL CHECK (status IN ('active','archived')),
    revision integer NOT NULL CHECK (revision > 0),
    created_by text NOT NULL REFERENCES radishnexus.users(id),
    updated_by text NOT NULL REFERENCES radishnexus.users(id),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (workspace_id,project_id,id),
    UNIQUE (workspace_id,project_id,name_key),
    FOREIGN KEY (workspace_id,project_id) REFERENCES radishnexus.projects(workspace_id,id)
);

CREATE TABLE radishnexus.ticket_label_assignments (
    workspace_id text NOT NULL,
    project_id text NOT NULL,
    ticket_id text NOT NULL,
    label_id text NOT NULL,
    PRIMARY KEY (workspace_id,project_id,ticket_id,label_id),
    FOREIGN KEY (workspace_id,project_id,ticket_id) REFERENCES radishnexus.tickets(workspace_id,governing_project_id,id),
    FOREIGN KEY (workspace_id,project_id,label_id) REFERENCES radishnexus.ticket_labels(workspace_id,project_id,id)
);
CREATE INDEX ticket_label_assignments_by_label ON radishnexus.ticket_label_assignments(workspace_id,project_id,label_id,ticket_id);

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
        WHEN 'ticket-label' THEN SELECT workspace_id INTO resolved_workspace FROM ticket_labels WHERE id=entity_id;
        WHEN 'ci-run' THEN SELECT workspace_id INTO resolved_workspace FROM ci_runs WHERE id=entity_id;
        WHEN 'deployment' THEN SELECT workspace_id INTO resolved_workspace FROM deployments WHERE id=entity_id;
        ELSE RETURN NULL;
    END CASE;
    RETURN resolved_workspace;
END;
$$;

ALTER TABLE radishnexus.collaboration_command_receipts
    DROP CONSTRAINT collaboration_command_receipts_kind,
    DROP CONSTRAINT collaboration_command_receipts_target,
    DROP CONSTRAINT collaboration_command_receipts_result,
    ADD CONSTRAINT collaboration_command_receipts_kind CHECK (command_kind IN (
        'decision.propose','decision.accept','decision.reject','decision.supersede','decision.review',
        'ticket.create','document.create','document.save','document.restore','deployment.record',
        'ticket.component.link','ticket.component.unlink','ticket.status.change','ticket.assignee.set','ticket.priority.set','ticket.due-date.set','ticket.create-from-message','ticket.discussion.link','ticket.discussion.unlink','ticket.classification.set','ticket-label.create','ticket-label.rename','ticket-label.archive')),
    ADD CONSTRAINT collaboration_command_receipts_target CHECK (
        (command_kind='decision.propose' AND target_type='thread') OR
        (command_kind='ticket.create-from-message' AND target_type='message') OR
        (command_kind IN ('decision.accept','decision.reject','decision.supersede','decision.review','ticket.create') AND target_type='decision') OR
        (command_kind='document.create' AND target_type='ticket') OR
        (command_kind IN ('document.save','document.restore') AND target_type='document') OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink','ticket.status.change','ticket.assignee.set','ticket.priority.set','ticket.due-date.set','ticket.discussion.link','ticket.discussion.unlink','ticket.classification.set') AND target_type='ticket') OR
        (command_kind='ticket-label.create' AND target_type='project') OR
        (command_kind IN ('ticket-label.rename','ticket-label.archive') AND target_type='ticket-label') OR
        (command_kind='deployment.record' AND target_type='ci-run')),
    ADD CONSTRAINT collaboration_command_receipts_result CHECK (
        (command_kind IN ('decision.propose','decision.accept','decision.reject') AND result_type='decision' AND result_revision IS NULL) OR
        (command_kind='decision.review' AND result_type='decision' AND result_id=target_id AND result_revision IS NULL) OR
        (command_kind IN ('ticket.create','ticket.create-from-message') AND result_type='ticket' AND result_revision IS NULL) OR
        (command_kind IN ('document.create','document.save','document.restore') AND result_type='document' AND result_revision IS NOT NULL AND result_revision>0) OR
        (command_kind='deployment.record' AND result_type='deployment' AND result_revision IS NULL) OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink','decision.supersede','ticket.discussion.link','ticket.discussion.unlink') AND result_type='entity-link' AND result_revision IS NULL) OR
        (command_kind='ticket-label.create' AND result_type='ticket-label' AND result_revision IS NOT NULL AND result_revision=1) OR
        (command_kind IN ('ticket-label.rename','ticket-label.archive') AND result_type='ticket-label' AND result_id=target_id AND result_revision IS NOT NULL AND result_revision>1) OR
        (command_kind IN ('ticket.status.change','ticket.assignee.set','ticket.priority.set','ticket.due-date.set','ticket.classification.set') AND result_type='ticket' AND result_id=target_id AND result_revision IS NOT NULL AND result_revision>1));

ALTER TABLE radishnexus.context_import_history DROP CONSTRAINT context_import_history_original_type_check,
    ADD CONSTRAINT context_import_history_original_type_check CHECK(original_type IN (
        'project.created','channel.created','message.created','thread.started','decision.proposed','decision.accepted',
        'decision.rejected','decision.superseded','decision.reviewed','ticket.created','ticket.status-changed',
        'ticket.assignee-changed','ticket.priority-changed','ticket.due-date-changed','ticket.discussion-linked','ticket.discussion-unlinked','ticket.classification-changed','ticket-label.created','ticket-label.renamed','ticket-label.archived','document.created','document.revised'));

ALTER TABLE radishnexus.context_import_mappings DROP CONSTRAINT context_import_mappings_kind_check,
    ADD CONSTRAINT context_import_mappings_kind_check CHECK (kind IN ('team','project','channel','message','thread','decision','ticket','ticket-label','document','actor','entity-link','event'));

DROP TRIGGER context_history_facts ON radishnexus.context_import_history;
CREATE CONSTRAINT TRIGGER context_history_facts AFTER INSERT ON radishnexus.context_import_history
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
    WHEN (NEW.original_version=1 AND NEW.original_type NOT IN ('decision.reviewed','ticket.due-date-changed','ticket.discussion-linked','ticket.discussion-unlinked','ticket.classification-changed','ticket-label.created','ticket-label.renamed','ticket-label.archived'))
    EXECUTE FUNCTION radishnexus.validate_context_facts();

UPDATE radishnexus.activity_items SET projection_version=16 WHERE projection_version=15;

-- Each new audit kind is scoped to a Project and a stable label reference.
DO $$
DECLARE constraint_name text; original text; extension text;
BEGIN
    FOREACH constraint_name IN ARRAY ARRAY['configuration_audit_scope','configuration_audit_result','configuration_audit_state'] LOOP
        SELECT pg_get_constraintdef(c.oid) INTO STRICT original FROM pg_constraint c
        WHERE c.conrelid='radishnexus.workspace_configuration_audit'::regclass AND c.conname=constraint_name;
        IF left(original,7)<>'CHECK (' OR right(original,1)<>')' THEN RAISE EXCEPTION 'Unexpected configuration constraint'; END IF;
        original=substring(original FROM 8 FOR length(original)-8);
        extension=$check$command_kind IN ('ticket-label.create','ticket-label.rename','ticket-label.archive')
            AND radishnexus.valid_entity_id('project',scope_id) AND radishnexus.valid_entity_id('ticket-label',subject_id)
            AND result_id=subject_id AND changed AND authorization_id IS NULL
            AND cardinality(granted_user_ids)=0 AND cardinality(removed_channel_ids)=0 AND cardinality(removed_thread_ids)=0
            AND ((command_kind='ticket-label.create' AND before_state='' AND after_state='active')
                OR (command_kind='ticket-label.rename' AND before_state='active' AND after_state='active')
                OR (command_kind='ticket-label.archive' AND before_state='active' AND after_state='archived'))$check$;
        EXECUTE format('ALTER TABLE radishnexus.workspace_configuration_audit DROP CONSTRAINT %I, ADD CONSTRAINT %I CHECK ((%s) OR (%s))',
            constraint_name,constraint_name,original,extension);
    END LOOP;
END;
$$;

CREATE INDEX ticket_classification_project_history ON radishnexus.domain_events(workspace_id,project_id,occurred_at DESC)
    WHERE event_type IN ('ticket-label.created','ticket-label.renamed','ticket-label.archived','ticket.classification-changed') AND actor_kind='user';
CREATE UNIQUE INDEX ticket_classification_initial_event ON radishnexus.domain_events(workspace_id,causation_id)
    WHERE event_type='ticket.classification-changed' AND causation_id IS NOT NULL;
CREATE UNIQUE INDEX ticket_classification_receipt_event ON radishnexus.collaboration_command_receipts (workspace_id,event_id)
    WHERE command_kind IN ('ticket.classification.set','ticket-label.create','ticket-label.rename','ticket-label.archive');
