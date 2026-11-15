ALTER TABLE radishnexus.collaboration_command_receipts
    DROP CONSTRAINT collaboration_command_receipts_kind,
    DROP CONSTRAINT collaboration_command_receipts_target,
    DROP CONSTRAINT collaboration_command_receipts_result,
    ADD CONSTRAINT collaboration_command_receipts_kind CHECK (command_kind IN (
        'decision.propose','decision.accept','decision.reject','decision.supersede','decision.review',
        'ticket.create','document.create','document.save','document.restore','deployment.record',
        'ticket.component.link','ticket.component.unlink','ticket.status.change','ticket.assignee.set','ticket.priority.set','ticket.due-date.set','ticket.create-from-message','ticket.discussion.link','ticket.discussion.unlink')),
    ADD CONSTRAINT collaboration_command_receipts_target CHECK (
        (command_kind='decision.propose' AND target_type='thread') OR
        (command_kind='ticket.create-from-message' AND target_type='message') OR
        (command_kind IN ('decision.accept','decision.reject','decision.supersede','decision.review','ticket.create') AND target_type='decision') OR
        (command_kind='document.create' AND target_type='ticket') OR
        (command_kind IN ('document.save','document.restore') AND target_type='document') OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink','ticket.status.change','ticket.assignee.set','ticket.priority.set','ticket.due-date.set','ticket.discussion.link','ticket.discussion.unlink') AND target_type='ticket') OR
        (command_kind='deployment.record' AND target_type='ci-run')),
    ADD CONSTRAINT collaboration_command_receipts_result CHECK (
        (command_kind IN ('decision.propose','decision.accept','decision.reject') AND result_type='decision' AND result_revision IS NULL) OR
        (command_kind='decision.review' AND result_type='decision' AND result_id=target_id AND result_revision IS NULL) OR
        (command_kind IN ('ticket.create','ticket.create-from-message') AND result_type='ticket' AND result_revision IS NULL) OR
        (command_kind IN ('document.create','document.save','document.restore') AND result_type='document' AND result_revision IS NOT NULL AND result_revision>0) OR
        (command_kind='deployment.record' AND result_type='deployment' AND result_revision IS NULL) OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink','decision.supersede','ticket.discussion.link','ticket.discussion.unlink') AND result_type='entity-link' AND result_revision IS NULL) OR
        (command_kind IN ('ticket.status.change','ticket.assignee.set','ticket.priority.set','ticket.due-date.set') AND result_type='ticket' AND result_id=target_id AND result_revision IS NOT NULL AND result_revision>1));

ALTER TABLE radishnexus.context_import_history DROP CONSTRAINT context_import_history_original_type_check,
    ADD CONSTRAINT context_import_history_original_type_check CHECK(original_type IN (
        'project.created','channel.created','message.created','thread.started','decision.proposed','decision.accepted',
        'decision.rejected','decision.superseded','decision.reviewed','ticket.created','ticket.status-changed',
        'ticket.assignee-changed','ticket.priority-changed','ticket.due-date-changed','ticket.discussion-linked','ticket.discussion-unlinked','document.created','document.revised'));

ALTER TABLE radishnexus.context_import_history
    ADD COLUMN original_version integer NOT NULL DEFAULT 1
    CHECK (original_version=1 OR (original_version=2 AND original_type='ticket.created'));

DROP TRIGGER context_history_facts ON radishnexus.context_import_history;
CREATE CONSTRAINT TRIGGER context_history_facts AFTER INSERT ON radishnexus.context_import_history
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
    WHEN (NEW.original_version=1 AND NEW.original_type NOT IN ('decision.reviewed','ticket.due-date-changed','ticket.discussion-linked','ticket.discussion-unlinked'))
    EXECUTE FUNCTION radishnexus.validate_context_facts();

CREATE OR REPLACE FUNCTION radishnexus.validate_context_link() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF NEW.origin='import' AND NOT EXISTS (
        SELECT 1 FROM context_import_mappings m JOIN context_import_history h
        ON h.workspace_id=m.workspace_id AND h.operation_id=m.operation_id AND h.event_id=NEW.source_event_id
        JOIN domain_events e ON e.workspace_id=h.workspace_id AND e.event_id=h.event_id
        WHERE m.workspace_id=NEW.workspace_id AND m.kind='entity-link' AND m.target_id=NEW.id
        AND NEW.created_by_kind='import' AND NEW.created_by_id=m.operation_id::text AND NEW.origin_ref=m.operation_id::text
        AND m.source_actor_id=h.original_actor AND NEW.created_at=h.original_time AND NEW.assertion='asserted' AND ((NEW.relation_type<>'supersedes' AND NEW.metadata='{}') OR (NEW.relation_type='supersedes' AND NEW.metadata=jsonb_build_object('supersession_reason',NEW.metadata->>'supersession_reason')))
        AND (
            (NEW.from_type='thread' AND NEW.relation_type='started-from' AND NEW.to_type='message' AND h.original_type='thread.started'
                AND e.primary_entity_type='thread' AND e.primary_entity_id=NEW.from_id AND h.original_facts->'message'=jsonb_build_object('type','message','id',NEW.to_id))
            OR (NEW.from_type='decision' AND NEW.relation_type='derived-from' AND NEW.to_type='thread' AND h.original_type='decision.proposed'
                AND e.primary_entity_type='decision' AND e.primary_entity_id=NEW.from_id AND h.original_facts->'evidence'=jsonb_build_object('type','thread','id',NEW.to_id))
            OR (NEW.from_type='decision' AND NEW.relation_type='supersedes' AND NEW.to_type='decision' AND h.original_type='decision.superseded' AND e.primary_entity_type='decision' AND e.primary_entity_id=NEW.to_id AND h.original_facts->'replacement'=jsonb_build_object('type','decision','id',NEW.from_id) AND h.original_facts->'relation'=jsonb_build_object('type','entity-link','id',NEW.id))
            OR (NEW.from_type='ticket' AND NEW.relation_type='implements' AND NEW.to_type='decision' AND h.original_type='ticket.created'
                AND e.primary_entity_type='ticket' AND e.primary_entity_id=NEW.from_id AND h.original_facts->'decision'=jsonb_build_object('type','decision','id',NEW.to_id))
            OR (NEW.from_type='ticket' AND NEW.relation_type='started-from' AND NEW.to_type='message' AND h.original_type='ticket.created'
                AND h.original_version=2 AND e.primary_entity_type='ticket' AND e.primary_entity_id=NEW.from_id AND h.original_facts->'message'=jsonb_build_object('type','message','id',NEW.to_id))
            OR (NEW.from_type='ticket' AND NEW.relation_type='relates-to' AND NEW.to_type='thread' AND h.original_type='ticket.discussion-linked'
                AND h.original_version=1 AND e.primary_entity_type='ticket' AND e.primary_entity_id=NEW.from_id AND h.original_facts->'thread'=jsonb_build_object('type','thread','id',NEW.to_id)
                AND h.original_facts->'relation'=jsonb_build_object('type','entity-link','id',NEW.id))
            OR (NEW.from_type='ticket' AND NEW.relation_type='relates-to' AND NEW.to_type='document' AND h.original_type='document.created'
                AND e.primary_entity_type='document' AND e.primary_entity_id=NEW.to_id AND h.original_facts->'ticket'=jsonb_build_object('type','ticket','id',NEW.from_id))
        )
    ) THEN RAISE EXCEPTION 'Imported link requires exact provenance' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;

INSERT INTO radishnexus.relation_types VALUES
    ('ticket','started-from','message'), ('ticket','relates-to','thread');

CREATE UNIQUE INDEX ticket_message_creation_source ON radishnexus.entity_links (workspace_id,from_id)
    WHERE from_type='ticket' AND relation_type='started-from' AND to_type='message';
CREATE UNIQUE INDEX ticket_discussion_active ON radishnexus.entity_links (workspace_id,from_id,to_id)
    WHERE from_type='ticket' AND relation_type='relates-to' AND to_type='thread' AND state='active';
CREATE INDEX ticket_execution_reverse ON radishnexus.entity_links (workspace_id,to_type,to_id,from_id)
    WHERE from_type='ticket' AND to_type IN ('message','thread') AND state='active';
CREATE INDEX thread_message_page ON radishnexus.messages (workspace_id,channel_id,thread_id,created_at DESC,id DESC)
    WHERE thread_id IS NOT NULL;
CREATE UNIQUE INDEX ticket_execution_receipt_event ON radishnexus.collaboration_command_receipts (workspace_id,event_id)
    WHERE command_kind IN ('ticket.create-from-message','ticket.discussion.link','ticket.discussion.unlink');

CREATE FUNCTION radishnexus.validate_ticket_execution_event(w text, id text) RETURNS void
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE e domain_events%ROWTYPE;
        h context_import_history%ROWTYPE;
        t tickets%ROWTYPE;
        l entity_links%ROWTYPE;
        kind text;
        version integer;
        facts jsonb;
        actor text;
        at_time timestamptz;
        command text;
        target_kind text;
        expected_target_id text;
        expected_result_id text;
BEGIN
    SELECT * INTO STRICT e FROM domain_events WHERE workspace_id=w AND event_id=id;
    SELECT * INTO h FROM context_import_history WHERE workspace_id=w AND event_id=id;
    kind := COALESCE(h.original_type,e.event_type);
    version := COALESCE(h.original_version,e.schema_version);
    facts := COALESCE(h.original_facts,e.payload);
    actor := COALESCE(h.original_actor,e.actor_id);
    at_time := COALESCE(h.original_time,e.occurred_at);
    SELECT * INTO STRICT t FROM tickets WHERE workspace_id=w AND tickets.id=e.primary_entity_id;
    IF e.primary_entity_type<>'ticket' OR e.project_id IS DISTINCT FROM t.governing_project_id OR e.actor_kind<>'user'
      OR at_time<t.created_at THEN
        RAISE EXCEPTION 'Invalid Ticket execution target' USING ERRCODE='23514';
    END IF;

    IF kind='ticket.created' AND version=2 THEN
        command := 'ticket.create-from-message';
        target_kind := 'message';
        expected_target_id := facts->'message'->>'id';
        expected_result_id := t.id;
        SELECT * INTO l FROM entity_links
        WHERE workspace_id=w AND from_type='ticket' AND from_id=t.id AND relation_type='started-from' AND to_type='message';
        IF l.id IS NULL OR l.state<>'active' OR l.source_event_id IS DISTINCT FROM id OR l.to_id IS DISTINCT FROM expected_target_id
           OR t.created_at<>at_time OR t.created_by IS DISTINCT FROM actor
           OR facts IS DISTINCT FROM jsonb_build_object('status','open','message',jsonb_build_object('type','message','id',expected_target_id))
           OR EXISTS (SELECT 1 FROM entity_links d WHERE d.workspace_id=w AND d.from_type='ticket' AND d.from_id=t.id AND d.relation_type='implements' AND d.to_type='decision')
           OR NOT EXISTS (
             SELECT 1 FROM messages m JOIN channels c ON c.workspace_id=m.workspace_id AND c.id=m.channel_id
             LEFT JOIN threads th ON th.workspace_id=m.workspace_id AND th.id=m.thread_id
             WHERE m.workspace_id=w AND m.id=expected_target_id AND m.created_at<=at_time
               AND c.governing_project_id=t.governing_project_id AND c.visibility='project'
               AND (m.thread_id IS NULL OR (th.visibility='project' AND th.origin_channel_id=c.id AND th.governing_project_id=c.governing_project_id))
           ) THEN RAISE EXCEPTION 'Invalid Message Ticket source' USING ERRCODE='23514'; END IF;
    ELSIF kind IN ('ticket.discussion-linked','ticket.discussion-unlinked') AND version=1 THEN
        command := CASE kind WHEN 'ticket.discussion-linked' THEN 'ticket.discussion.link' ELSE 'ticket.discussion.unlink' END;
        target_kind := 'ticket';
        expected_target_id := t.id;
        expected_result_id := facts->'relation'->>'id';
        SELECT * INTO l FROM entity_links WHERE workspace_id=w AND entity_links.id=expected_result_id;
        IF l.id IS NULL OR l.from_type<>'ticket' OR l.from_id<>t.id OR l.relation_type<>'relates-to' OR l.to_type<>'thread'
           OR facts IS DISTINCT FROM jsonb_build_object('thread',jsonb_build_object('type','thread','id',l.to_id),'relation',jsonb_build_object('type','entity-link','id',l.id))
           OR NOT EXISTS (SELECT 1 FROM threads th JOIN channels c ON c.workspace_id=th.workspace_id AND c.id=th.origin_channel_id
                 WHERE th.workspace_id=w AND th.id=l.to_id AND th.governing_project_id=t.governing_project_id
                   AND c.governing_project_id=t.governing_project_id AND th.created_at<=at_time)
           OR (kind='ticket.discussion-linked' AND (l.source_event_id IS DISTINCT FROM id OR l.created_at<>at_time))
           OR (kind='ticket.discussion-unlinked' AND (l.state<>'removed' OR l.removed_at IS DISTINCT FROM at_time OR l.removed_by_id IS DISTINCT FROM actor OR l.removal_reason IS DISTINCT FROM 'ticket-discussion-unlinked' OR at_time<=l.created_at))
         THEN RAISE EXCEPTION 'Invalid Ticket discussion facts' USING ERRCODE='23514'; END IF;
    ELSE
        RAISE EXCEPTION 'Unsupported Ticket execution event' USING ERRCODE='23514';
    END IF;

    IF h.event_id IS NULL THEN
        IF e.source_kind<>'web' OR l.assertion<>'asserted' OR l.metadata<>'{}'
           OR (kind<>'ticket.discussion-unlinked' AND (l.origin<>'user' OR l.created_by_kind<>'user' OR l.created_by_id IS DISTINCT FROM actor OR l.created_at<>at_time))
           OR NOT EXISTS (SELECT 1 FROM collaboration_command_receipts r WHERE r.workspace_id=w AND r.event_id=id
               AND r.command_kind=command AND r.target_type=target_kind AND r.target_id=expected_target_id AND r.result_id=expected_result_id
               AND r.actor_id=actor AND r.created_at=at_time)
        THEN RAISE EXCEPTION 'Ticket execution requires exact receipt' USING ERRCODE='23514'; END IF;
    ELSE
        IF e.event_type<>'context.history-recorded' OR e.source_kind<>'import'
           OR (kind<>'ticket.discussion-unlinked' AND (l.origin<>'import' OR l.origin_ref IS DISTINCT FROM h.operation_id::text))
           OR EXISTS (SELECT 1 FROM collaboration_command_receipts r WHERE r.workspace_id=w AND r.event_id=id)
        THEN RAISE EXCEPTION 'Invalid imported Ticket execution provenance' USING ERRCODE='23514'; END IF;
    END IF;
END;
$$;

CREATE FUNCTION radishnexus.check_ticket_execution_event() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    PERFORM validate_ticket_execution_event(NEW.workspace_id,NEW.event_id);
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER ticket_execution_events AFTER INSERT ON radishnexus.domain_events
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
    WHEN ((NEW.event_type='ticket.created' AND NEW.schema_version=2) OR NEW.event_type IN ('ticket.discussion-linked','ticket.discussion-unlinked'))
    EXECUTE FUNCTION radishnexus.check_ticket_execution_event();
CREATE CONSTRAINT TRIGGER ticket_execution_receipts AFTER INSERT ON radishnexus.collaboration_command_receipts
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.command_kind IN ('ticket.create-from-message','ticket.discussion.link','ticket.discussion.unlink'))
    EXECUTE FUNCTION radishnexus.check_ticket_execution_event();
CREATE CONSTRAINT TRIGGER ticket_execution_history AFTER INSERT ON radishnexus.context_import_history
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.original_version=2 OR NEW.original_type IN ('ticket.discussion-linked','ticket.discussion-unlinked'))
    EXECUTE FUNCTION radishnexus.check_ticket_execution_event();

CREATE FUNCTION radishnexus.guard_ticket_execution_link() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE removal_event text;
BEGIN
    IF NEW.from_type='ticket' AND NEW.relation_type='implements' AND NEW.to_type='decision' THEN
        IF EXISTS(SELECT 1 FROM entity_links l WHERE l.workspace_id=NEW.workspace_id AND l.from_type='ticket'
             AND l.from_id=NEW.from_id AND l.relation_type='started-from' AND l.to_type='message') THEN
           RAISE EXCEPTION 'Ticket has a Message source' USING ERRCODE='23514';
        END IF;
    ELSE
        IF NOT EXISTS (
            SELECT 1 FROM domain_events e LEFT JOIN context_import_history h ON h.workspace_id=e.workspace_id AND h.event_id=e.event_id
            WHERE e.workspace_id=NEW.workspace_id AND e.event_id=NEW.source_event_id AND e.primary_entity_type='ticket' AND e.primary_entity_id=NEW.from_id
              AND ((NEW.to_type='message' AND COALESCE(h.original_type,e.event_type)='ticket.created' AND COALESCE(h.original_version,e.schema_version)=2)
                OR (NEW.to_type='thread' AND COALESCE(h.original_type,e.event_type)='ticket.discussion-linked'
                    AND COALESCE(h.original_facts,e.payload)->'relation'->>'id'=NEW.id))
        ) THEN RAISE EXCEPTION 'Invalid Ticket execution link source' USING ERRCODE='23514'; END IF;
        PERFORM validate_ticket_execution_event(NEW.workspace_id,NEW.source_event_id);
        IF NEW.state='removed' AND NEW.to_type='thread' THEN
            SELECT e.event_id INTO STRICT removal_event FROM domain_events e
            LEFT JOIN context_import_history h ON h.workspace_id=e.workspace_id AND h.event_id=e.event_id
            WHERE e.workspace_id=NEW.workspace_id AND e.primary_entity_type='ticket' AND e.primary_entity_id=NEW.from_id
              AND COALESCE(h.original_type,e.event_type)='ticket.discussion-unlinked'
              AND COALESCE(h.original_facts,e.payload)->'relation'->>'id'=NEW.id;
            PERFORM validate_ticket_execution_event(NEW.workspace_id,removal_event);
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER ticket_execution_link_facts AFTER INSERT OR UPDATE ON radishnexus.entity_links
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.from_type='ticket' AND
       ((NEW.relation_type='started-from' AND NEW.to_type='message') OR
        (NEW.relation_type='relates-to' AND NEW.to_type='thread') OR
        (NEW.relation_type='implements' AND NEW.to_type='decision')))
    EXECUTE FUNCTION radishnexus.guard_ticket_execution_link();

UPDATE radishnexus.activity_items SET projection_version=15 WHERE projection_version=14;
---- create above / drop below ----
-- Forward-only; restore a matching artifact and backup into an empty target.
