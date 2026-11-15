INSERT INTO radishnexus.relation_types(from_type,relation_type,to_type) VALUES ('ticket','tracks-build','ci-run');

ALTER TABLE radishnexus.entity_links ADD CONSTRAINT ticket_ci_run_provenance CHECK (
    relation_type <> 'tracks-build' OR (
        from_type='ticket' AND to_type='ci-run' AND assertion='asserted'
        AND origin='user' AND origin_ref IS NULL AND created_by_kind='user' AND source_event_id IS NOT NULL
        AND metadata='{}'::jsonb
        AND ((state='active' AND updated_at=created_at) OR (state='removed' AND removed_by_kind='user'
            AND removal_reason='ticket-ci-run-unlinked' AND updated_at=removed_at AND removed_at>created_at))
    )
);
CREATE UNIQUE INDEX ticket_ci_run_active ON radishnexus.entity_links(workspace_id,from_id,to_id)
    WHERE relation_type='tracks-build' AND state='active';
CREATE INDEX ci_run_tickets_active ON radishnexus.entity_links(workspace_id,to_id,from_id)
    WHERE relation_type='tracks-build' AND state='active';
CREATE INDEX ticket_ci_run_last_change ON radishnexus.entity_links(workspace_id,from_id,updated_at DESC)
    WHERE relation_type='tracks-build';
CREATE INDEX component_ci_runs_by_id ON radishnexus.ci_runs(workspace_id,component_id,id)
    WHERE status IN ('succeeded','failed','canceled');
CREATE INDEX ci_run_deployments_by_id ON radishnexus.deployments(workspace_id,ci_run_id,id);
CREATE INDEX ticket_ci_run_history_page ON radishnexus.activity_items(workspace_id,target_id,projection_version,occurred_at DESC,event_id DESC)
    WHERE target_type='ticket' AND activity_type IN ('ticket.ci-run-linked','ticket.ci-run-unlinked');

ALTER TABLE radishnexus.collaboration_command_receipts
    DROP CONSTRAINT collaboration_command_receipts_kind,
    DROP CONSTRAINT collaboration_command_receipts_target,
    DROP CONSTRAINT collaboration_command_receipts_result,
    ADD CONSTRAINT collaboration_command_receipts_kind CHECK (command_kind IN (
        'decision.propose','decision.accept','decision.reject','decision.supersede','decision.review',
        'ticket.create','document.create','document.save','document.restore','deployment.record',
        'ticket.component.link','ticket.component.unlink','ticket.ci-run.link','ticket.ci-run.unlink','ticket.status.change','ticket.assignee.set','ticket.priority.set','ticket.due-date.set','ticket.create-from-message','ticket.discussion.link','ticket.discussion.unlink','ticket.classification.set','ticket-label.create','ticket-label.rename','ticket-label.archive')),
    ADD CONSTRAINT collaboration_command_receipts_target CHECK (
        (command_kind='decision.propose' AND target_type='thread') OR
        (command_kind='ticket.create-from-message' AND target_type='message') OR
        (command_kind IN ('decision.accept','decision.reject','decision.supersede','decision.review','ticket.create') AND target_type='decision') OR
        (command_kind='document.create' AND target_type='ticket') OR
        (command_kind IN ('document.save','document.restore') AND target_type='document') OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink','ticket.ci-run.link','ticket.ci-run.unlink','ticket.status.change','ticket.assignee.set','ticket.priority.set','ticket.due-date.set','ticket.discussion.link','ticket.discussion.unlink','ticket.classification.set') AND target_type='ticket') OR
        (command_kind='ticket-label.create' AND target_type='project') OR
        (command_kind IN ('ticket-label.rename','ticket-label.archive') AND target_type='ticket-label') OR
        (command_kind='deployment.record' AND target_type='ci-run')),
    ADD CONSTRAINT collaboration_command_receipts_result CHECK (
        (command_kind IN ('decision.propose','decision.accept','decision.reject') AND result_type='decision' AND result_revision IS NULL) OR
        (command_kind='decision.review' AND result_type='decision' AND result_id=target_id AND result_revision IS NULL) OR
        (command_kind IN ('ticket.create','ticket.create-from-message') AND result_type='ticket' AND result_revision IS NULL) OR
        (command_kind IN ('document.create','document.save','document.restore') AND result_type='document' AND result_revision IS NOT NULL AND result_revision>0) OR
        (command_kind='deployment.record' AND result_type='deployment' AND result_revision IS NULL) OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink','ticket.ci-run.link','ticket.ci-run.unlink','decision.supersede','ticket.discussion.link','ticket.discussion.unlink') AND result_type='entity-link' AND result_revision IS NULL) OR
        (command_kind='ticket-label.create' AND result_type='ticket-label' AND result_revision IS NOT NULL AND result_revision=1) OR
        (command_kind IN ('ticket-label.rename','ticket-label.archive') AND result_type='ticket-label' AND result_id=target_id AND result_revision IS NOT NULL AND result_revision>1) OR
        (command_kind IN ('ticket.status.change','ticket.assignee.set','ticket.priority.set','ticket.due-date.set','ticket.classification.set') AND result_type='ticket' AND result_id=target_id AND result_revision IS NOT NULL AND result_revision>1));

CREATE UNIQUE INDEX ticket_ci_run_receipt_event ON radishnexus.collaboration_command_receipts(workspace_id,event_id)
    WHERE command_kind IN ('ticket.ci-run.link','ticket.ci-run.unlink');
CREATE UNIQUE INDEX ticket_ci_run_receipt_relation ON radishnexus.collaboration_command_receipts(workspace_id,result_id,command_kind)
    WHERE command_kind IN ('ticket.ci-run.link','ticket.ci-run.unlink');

-- Accept an original link receipt after that exact relationship has been removed.
CREATE FUNCTION radishnexus.ticket_ci_run_operation_matches(w text, event text) RETURNS boolean
LANGUAGE sql STABLE SET search_path=radishnexus,pg_temp AS $$
    SELECT EXISTS (
        SELECT 1 FROM collaboration_command_receipts r
        JOIN entity_links l ON (l.workspace_id,l.id)=(r.workspace_id,r.result_id)
        JOIN tickets t ON (t.workspace_id,t.id)=(l.workspace_id,l.from_id)
        JOIN ci_runs c ON (c.workspace_id,c.id)=(l.workspace_id,l.to_id)
        JOIN domain_events e ON (e.workspace_id,e.event_id)=(r.workspace_id,r.event_id)
        WHERE r.workspace_id=w AND r.event_id=event AND r.target_type='ticket' AND r.target_id=t.id
        AND r.result_type='entity-link' AND l.relation_type='tracks-build'
        AND c.status IN ('succeeded','failed','canceled') AND c.completed_at<=l.created_at
        AND e.primary_entity_type='ticket' AND e.primary_entity_id=t.id AND e.project_id=t.governing_project_id
        AND e.actor_kind='user' AND e.actor_id=r.actor_id AND e.source_kind='web' AND e.schema_version=1 AND e.occurred_at=r.created_at
        AND e.payload=jsonb_build_object('ci_run',jsonb_build_object('type','ci-run','id',l.to_id),
            'link',jsonb_build_object('type','entity-link','id',l.id),
            'state',CASE WHEN r.command_kind='ticket.ci-run.link' THEN 'active' ELSE 'removed' END)
        AND (
            (r.command_kind='ticket.ci-run.link' AND e.event_type='ticket.ci-run-linked'
             AND l.source_event_id=e.event_id AND l.created_by_id=r.actor_id AND l.created_at=r.created_at)
            OR (r.command_kind='ticket.ci-run.unlink' AND e.event_type='ticket.ci-run-unlinked'
             AND l.state='removed' AND l.removed_by_id=r.actor_id AND l.removed_at=r.created_at AND e.causation_id=l.source_event_id)
        )
    );
$$;

CREATE FUNCTION radishnexus.validate_ticket_ci_run_provenance() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE event text;
BEGIN
    IF TG_TABLE_NAME='entity_links' THEN
        IF NOT ticket_ci_run_operation_matches(NEW.workspace_id,NEW.source_event_id) THEN
            RAISE EXCEPTION 'Ticket CI Run link requires creation provenance' USING ERRCODE='23514';
        END IF;
        IF NEW.state='removed' THEN
            SELECT r.event_id INTO event FROM collaboration_command_receipts r
            WHERE r.workspace_id=NEW.workspace_id AND r.result_id=NEW.id AND r.command_kind='ticket.ci-run.unlink';
            IF NOT ticket_ci_run_operation_matches(NEW.workspace_id,event) THEN
                RAISE EXCEPTION 'Ticket CI Run link requires removal provenance' USING ERRCODE='23514';
            END IF;
        END IF;
    ELSIF NOT ticket_ci_run_operation_matches(NEW.workspace_id,NEW.event_id) THEN
        RAISE EXCEPTION 'Ticket CI Run operation provenance mismatch' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER ticket_ci_run_link_provenance AFTER INSERT OR UPDATE ON radishnexus.entity_links
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.relation_type='tracks-build')
    EXECUTE FUNCTION radishnexus.validate_ticket_ci_run_provenance();
CREATE CONSTRAINT TRIGGER ticket_ci_run_receipt_provenance AFTER INSERT ON radishnexus.collaboration_command_receipts
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.command_kind IN ('ticket.ci-run.link','ticket.ci-run.unlink'))
    EXECUTE FUNCTION radishnexus.validate_ticket_ci_run_provenance();
CREATE CONSTRAINT TRIGGER ticket_ci_run_event_provenance AFTER INSERT ON radishnexus.domain_events
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.event_type IN ('ticket.ci-run-linked','ticket.ci-run-unlinked'))
    EXECUTE FUNCTION radishnexus.validate_ticket_ci_run_provenance();

UPDATE radishnexus.activity_items SET projection_version=17 WHERE projection_version=16;
