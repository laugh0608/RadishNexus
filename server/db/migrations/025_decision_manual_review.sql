ALTER TABLE radishnexus.collaboration_command_receipts
    DROP CONSTRAINT collaboration_command_receipts_kind,
    DROP CONSTRAINT collaboration_command_receipts_target,
    DROP CONSTRAINT collaboration_command_receipts_result,
    ADD CONSTRAINT collaboration_command_receipts_kind CHECK (command_kind IN (
        'decision.propose','decision.accept','decision.reject','decision.supersede','decision.review',
        'ticket.create','document.create','document.save','document.restore','deployment.record',
        'ticket.component.link','ticket.component.unlink','ticket.status.change','ticket.assignee.set','ticket.priority.set')),
    ADD CONSTRAINT collaboration_command_receipts_target CHECK (
        (command_kind='decision.propose' AND target_type='thread') OR
        (command_kind IN ('decision.accept','decision.reject','decision.supersede','decision.review','ticket.create') AND target_type='decision') OR
        (command_kind='document.create' AND target_type='ticket') OR
        (command_kind IN ('document.save','document.restore') AND target_type='document') OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink','ticket.status.change','ticket.assignee.set','ticket.priority.set') AND target_type='ticket') OR
        (command_kind='deployment.record' AND target_type='ci-run')),
    ADD CONSTRAINT collaboration_command_receipts_result CHECK (
        (command_kind IN ('decision.propose','decision.accept','decision.reject') AND result_type='decision' AND result_revision IS NULL) OR
        (command_kind='decision.review' AND result_type='decision' AND result_id=target_id AND result_revision IS NULL) OR
        (command_kind='ticket.create' AND result_type='ticket' AND result_revision IS NULL) OR
        (command_kind IN ('document.create','document.save','document.restore') AND result_type='document' AND result_revision IS NOT NULL AND result_revision>0) OR
        (command_kind='deployment.record' AND result_type='deployment' AND result_revision IS NULL) OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink','decision.supersede') AND result_type='entity-link' AND result_revision IS NULL) OR
        (command_kind IN ('ticket.status.change','ticket.assignee.set','ticket.priority.set') AND result_type='ticket' AND result_id=target_id AND result_revision IS NOT NULL AND result_revision>1));

CREATE UNIQUE INDEX decision_review_receipt_event ON radishnexus.collaboration_command_receipts (workspace_id,event_id)
    WHERE command_kind='decision.review';

ALTER TABLE radishnexus.context_import_history DROP CONSTRAINT context_import_history_original_type_check,
    ADD CONSTRAINT context_import_history_original_type_check CHECK(original_type IN (
        'project.created','channel.created','message.created','thread.started','decision.proposed','decision.accepted',
        'decision.rejected','decision.superseded','decision.reviewed','ticket.created','ticket.status-changed',
        'ticket.assignee-changed','ticket.priority-changed','document.created','document.revised'));

DROP TRIGGER context_history_facts ON radishnexus.context_import_history;
CREATE CONSTRAINT TRIGGER context_history_facts AFTER INSERT ON radishnexus.context_import_history
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
    WHEN (NEW.original_type <> 'decision.reviewed')
    EXECUTE FUNCTION radishnexus.validate_context_facts();

CREATE FUNCTION radishnexus.validate_decision_review(w text, id text) RETURNS void
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE e domain_events%ROWTYPE;
        h context_import_history%ROWTYPE;
        facts jsonb;
        evidence jsonb;
        recorded_at timestamptz;
BEGIN
    SELECT * INTO STRICT e FROM domain_events WHERE workspace_id=w AND event_id=id;
    SELECT * INTO h FROM context_import_history WHERE workspace_id=w AND event_id=id;
    IF e.schema_version <> 1 OR e.primary_entity_type <> 'decision' OR NOT EXISTS (
        SELECT 1 FROM decisions d WHERE d.workspace_id=w AND d.id=e.primary_entity_id AND d.governing_project_id=e.project_id
    ) THEN
        RAISE EXCEPTION 'Invalid Decision review target' USING ERRCODE='23514';
    END IF;

    IF h.event_id IS NULL THEN
        IF e.event_type <> 'decision.reviewed' OR e.actor_kind <> 'user' OR e.source_kind <> 'web' OR NOT EXISTS (
            SELECT 1 FROM collaboration_command_receipts r
            WHERE r.workspace_id=w AND r.event_id=id AND r.command_kind='decision.review'
              AND r.target_id=e.primary_entity_id AND r.actor_id=e.actor_id AND r.created_at=e.occurred_at
        ) THEN
            RAISE EXCEPTION 'Invalid native Decision review receipt' USING ERRCODE='23514';
        END IF;
        facts := e.payload;
        recorded_at := e.occurred_at;
    ELSE
        IF h.original_type <> 'decision.reviewed' OR e.event_type <> 'context.history-recorded'
            OR e.source_kind <> 'import' OR e.source_id IS DISTINCT FROM h.operation_id::text
            OR EXISTS (SELECT 1 FROM collaboration_command_receipts r WHERE r.workspace_id=w AND r.event_id=id)
        THEN
            RAISE EXCEPTION 'Invalid imported Decision review provenance' USING ERRCODE='23514';
        END IF;
        facts := h.original_facts;
        recorded_at := h.original_time;
    END IF;

    evidence := facts->'additional_evidence';
    IF facts IS DISTINCT FROM jsonb_build_object('decision_revision',facts->'decision_revision',
        'trigger','manual','conclusion',facts->'conclusion','reason',facts->'reason','additional_evidence',evidence)
        OR COALESCE(facts->'decision_revision' NOT IN ('2'::jsonb,'3'::jsonb),true)
        OR COALESCE(facts->>'conclusion' NOT IN ('still_applicable','reconsider'),true)
        OR jsonb_typeof(facts->'reason') IS DISTINCT FROM 'string'
        OR NOT valid_supersession_reason(facts->>'reason')
        OR octet_length(facts->>'reason') > 4096
        OR NOT EXISTS (
            SELECT 1 FROM entity_links l JOIN threads t ON t.workspace_id=l.workspace_id AND t.id=l.to_id
            WHERE l.workspace_id=w AND l.from_type='decision' AND l.from_id=e.primary_entity_id
              AND l.relation_type='derived-from' AND l.to_type='thread' AND l.state='active'
              AND t.governing_project_id=e.project_id AND t.created_at<recorded_at
        ) THEN
        RAISE EXCEPTION 'Invalid Decision review facts' USING ERRCODE='23514';
    END IF;
    IF h.event_id IS NULL AND NOT EXISTS (
        SELECT 1 FROM decisions d WHERE d.workspace_id=w AND d.id=e.primary_entity_id
          AND d.status='accepted' AND d.decision_revision=(facts->>'decision_revision')::integer
          AND d.updated_at<recorded_at
    ) THEN
        RAISE EXCEPTION 'Invalid native Decision review revision' USING ERRCODE='23514';
    END IF;
    IF evidence IS DISTINCT FROM 'null'::jsonb AND (
        evidence IS DISTINCT FROM jsonb_build_object('type','thread','id',evidence->>'id')
        OR NOT EXISTS (
            SELECT 1 FROM threads t WHERE t.workspace_id=w AND t.id=evidence->>'id'
                AND t.governing_project_id=e.project_id AND t.created_at<recorded_at
        )
        OR EXISTS (
            SELECT 1 FROM entity_links l WHERE l.workspace_id=w AND l.from_type='decision' AND l.from_id=e.primary_entity_id
                AND l.relation_type='derived-from' AND l.to_type='thread' AND l.to_id=evidence->>'id' AND l.state='active'
        )
        OR (h.event_id IS NOT NULL AND NOT EXISTS (
            SELECT 1 FROM context_import_mappings m WHERE m.workspace_id=w AND m.operation_id=h.operation_id
                AND m.kind='thread' AND m.target_id=evidence->>'id'
        ))
    ) THEN
        RAISE EXCEPTION 'Invalid Decision review evidence' USING ERRCODE='23514';
    END IF;
END;
$$;

CREATE FUNCTION radishnexus.check_decision_review() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    PERFORM validate_decision_review(NEW.workspace_id,NEW.event_id);
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER decision_review_event AFTER INSERT ON radishnexus.domain_events
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.event_type='decision.reviewed')
    EXECUTE FUNCTION radishnexus.check_decision_review();
CREATE CONSTRAINT TRIGGER decision_review_receipt AFTER INSERT ON radishnexus.collaboration_command_receipts
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.command_kind='decision.review')
    EXECUTE FUNCTION radishnexus.check_decision_review();
CREATE CONSTRAINT TRIGGER decision_review_history AFTER INSERT ON radishnexus.context_import_history
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.original_type='decision.reviewed')
    EXECUTE FUNCTION radishnexus.check_decision_review();

CREATE INDEX decision_review_activity_timeline ON radishnexus.activity_items
    (workspace_id,target_id,projection_version,occurred_at DESC,event_id DESC)
    WHERE activity_type='decision.reviewed' AND target_type='decision';
CREATE INDEX decision_review_event_time ON radishnexus.domain_events (workspace_id,primary_entity_id,occurred_at DESC)
    WHERE event_type='decision.reviewed' AND primary_entity_type='decision';

CREATE FUNCTION radishnexus.last_decision_review(w text, ids text[]) RETURNS timestamptz
LANGUAGE sql STABLE SET search_path=radishnexus,pg_temp AS $$
    SELECT GREATEST(
        (SELECT max(e.occurred_at) FROM domain_events e
         WHERE e.workspace_id=w AND e.primary_entity_type='decision'
           AND e.primary_entity_id=ANY(ids) AND e.event_type='decision.reviewed'),
        (SELECT max(h.original_time) FROM context_import_history h JOIN domain_events e
         ON e.workspace_id=h.workspace_id AND e.event_id=h.event_id
         WHERE h.workspace_id=w AND h.original_type='decision.reviewed'
           AND e.primary_entity_type='decision' AND e.primary_entity_id=ANY(ids))
    );
$$;
CREATE INDEX decision_review_imported_history ON radishnexus.context_import_history (workspace_id,event_id)
    WHERE original_type='decision.reviewed';

UPDATE radishnexus.activity_items SET projection_version=13 WHERE projection_version=12;
---- create above / drop below ----
-- Forward-only; restore a matching artifact and backup into an empty target.
