-- ADR-0053: store a civil date without a time zone or deadline instant.
ALTER TABLE radishnexus.tickets
    ADD COLUMN due_date date CHECK (due_date BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
    ADD COLUMN due_date_revision integer NOT NULL DEFAULT 1 CHECK (due_date_revision > 0);

ALTER TABLE radishnexus.collaboration_command_receipts
    DROP CONSTRAINT collaboration_command_receipts_kind,
    DROP CONSTRAINT collaboration_command_receipts_target,
    DROP CONSTRAINT collaboration_command_receipts_result,
    ADD CONSTRAINT collaboration_command_receipts_kind CHECK (command_kind IN (
        'decision.propose','decision.accept','decision.reject','decision.supersede','decision.review',
        'ticket.create','document.create','document.save','document.restore','deployment.record',
        'ticket.component.link','ticket.component.unlink','ticket.status.change','ticket.assignee.set','ticket.priority.set','ticket.due-date.set')),
    ADD CONSTRAINT collaboration_command_receipts_target CHECK (
        (command_kind='decision.propose' AND target_type='thread') OR
        (command_kind IN ('decision.accept','decision.reject','decision.supersede','decision.review','ticket.create') AND target_type='decision') OR
        (command_kind='document.create' AND target_type='ticket') OR
        (command_kind IN ('document.save','document.restore') AND target_type='document') OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink','ticket.status.change','ticket.assignee.set','ticket.priority.set','ticket.due-date.set') AND target_type='ticket') OR
        (command_kind='deployment.record' AND target_type='ci-run')),
    ADD CONSTRAINT collaboration_command_receipts_result CHECK (
        (command_kind IN ('decision.propose','decision.accept','decision.reject') AND result_type='decision' AND result_revision IS NULL) OR
        (command_kind='decision.review' AND result_type='decision' AND result_id=target_id AND result_revision IS NULL) OR
        (command_kind='ticket.create' AND result_type='ticket' AND result_revision IS NULL) OR
        (command_kind IN ('document.create','document.save','document.restore') AND result_type='document' AND result_revision IS NOT NULL AND result_revision>0) OR
        (command_kind='deployment.record' AND result_type='deployment' AND result_revision IS NULL) OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink','decision.supersede') AND result_type='entity-link' AND result_revision IS NULL) OR
        (command_kind IN ('ticket.status.change','ticket.assignee.set','ticket.priority.set','ticket.due-date.set') AND result_type='ticket' AND result_id=target_id AND result_revision IS NOT NULL AND result_revision>1));

CREATE UNIQUE INDEX ticket_due_date_receipt_event ON radishnexus.collaboration_command_receipts (workspace_id,event_id)
    WHERE command_kind='ticket.due-date.set';

ALTER TABLE radishnexus.context_import_history DROP CONSTRAINT context_import_history_original_type_check,
    ADD CONSTRAINT context_import_history_original_type_check CHECK(original_type IN (
        'project.created','channel.created','message.created','thread.started','decision.proposed','decision.accepted',
        'decision.rejected','decision.superseded','decision.reviewed','ticket.created','ticket.status-changed',
        'ticket.assignee-changed','ticket.priority-changed','ticket.due-date-changed','document.created','document.revised'));

DROP TRIGGER context_history_facts ON radishnexus.context_import_history;
CREATE CONSTRAINT TRIGGER context_history_facts AFTER INSERT ON radishnexus.context_import_history
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
    WHEN (NEW.original_type NOT IN ('decision.reviewed','ticket.due-date-changed'))
    EXECUTE FUNCTION radishnexus.validate_context_facts();

CREATE FUNCTION radishnexus.valid_ticket_due_date(value jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE canonical text;
BEGIN
    IF value = 'null'::jsonb THEN RETURN true; END IF;
    IF jsonb_typeof(value) IS DISTINCT FROM 'string' THEN RETURN false; END IF;
    canonical := value #>> '{}';
    RETURN canonical ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
        AND canonical::date BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'
        AND to_char(canonical::date::timestamp,'YYYY-MM-DD') = canonical;
EXCEPTION WHEN invalid_datetime_format OR datetime_field_overflow THEN
    RETURN false;
END;
$$;

CREATE FUNCTION radishnexus.valid_ticket_due_date_change(facts jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SET search_path=radishnexus,pg_temp AS $$
DECLARE change jsonb := facts->'due_date';
        before_revision integer;
        after_revision integer;
BEGIN
    IF jsonb_typeof(change) IS DISTINCT FROM 'object' THEN RETURN false; END IF;
    before_revision := (change->>'from_revision')::integer;
    after_revision := (change->>'to_revision')::integer;
    RETURN COALESCE(facts = jsonb_build_object('due_date',jsonb_build_object(
        'from',change->'from','to',change->'to','from_revision',before_revision,'to_revision',after_revision))
        AND valid_ticket_due_date(change->'from') AND valid_ticket_due_date(change->'to')
        AND change->'from' <> change->'to'
        AND before_revision > 0 AND before_revision < 2147483647
        AND after_revision::bigint = before_revision::bigint + 1,false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN
    RETURN false;
END;
$$;

CREATE FUNCTION radishnexus.check_ticket_due_date_change() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE event domain_events%ROWTYPE;
        history context_import_history%ROWTYPE;
        facts jsonb;
BEGIN
    SELECT * INTO STRICT event FROM domain_events WHERE workspace_id=NEW.workspace_id AND event_id=NEW.event_id;
    SELECT * INTO history FROM context_import_history WHERE workspace_id=NEW.workspace_id AND event_id=NEW.event_id;
    IF history.event_id IS NULL THEN
        facts := event.payload;
        IF event.event_type <> 'ticket.due-date-changed' OR event.actor_kind <> 'user' OR event.source_kind <> 'web'
            OR NOT EXISTS (
                SELECT 1 FROM collaboration_command_receipts r
                WHERE r.workspace_id=event.workspace_id AND r.event_id=event.event_id
                  AND r.command_kind='ticket.due-date.set' AND r.target_id=event.primary_entity_id
                  AND r.actor_id=event.actor_id AND r.created_at=event.occurred_at
                  AND to_jsonb(r.result_revision)=facts->'due_date'->'to_revision'
            ) THEN RAISE EXCEPTION 'Ticket due date event requires exact receipt' USING ERRCODE='23514'; END IF;
    ELSE
        facts := history.original_facts;
        IF history.original_type <> 'ticket.due-date-changed' OR event.event_type <> 'context.history-recorded'
            OR event.source_kind <> 'import' OR event.source_id IS DISTINCT FROM history.operation_id::text
            OR EXISTS (SELECT 1 FROM collaboration_command_receipts r WHERE r.workspace_id=event.workspace_id AND r.event_id=event.event_id)
        THEN RAISE EXCEPTION 'Invalid imported Ticket due date event' USING ERRCODE='23514'; END IF;
    END IF;
    IF event.schema_version <> 1 OR event.primary_entity_type <> 'ticket' OR NOT valid_ticket_due_date_change(facts)
        OR NOT EXISTS (
            SELECT 1 FROM tickets t WHERE t.workspace_id=event.workspace_id AND t.id=event.primary_entity_id
              AND t.governing_project_id=event.project_id
              AND t.due_date_revision >= (facts->'due_date'->>'to_revision')::integer
        ) THEN RAISE EXCEPTION 'Invalid Ticket due date facts' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER ticket_due_date_event AFTER INSERT ON radishnexus.domain_events
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.event_type='ticket.due-date-changed')
    EXECUTE FUNCTION radishnexus.check_ticket_due_date_change();
CREATE CONSTRAINT TRIGGER ticket_due_date_receipt AFTER INSERT ON radishnexus.collaboration_command_receipts
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.command_kind='ticket.due-date.set')
    EXECUTE FUNCTION radishnexus.check_ticket_due_date_change();
CREATE CONSTRAINT TRIGGER ticket_due_date_history AFTER INSERT ON radishnexus.context_import_history
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.original_type='ticket.due-date-changed')
    EXECUTE FUNCTION radishnexus.check_ticket_due_date_change();

CREATE UNIQUE INDEX ticket_due_date_event_revision ON radishnexus.domain_events
    (workspace_id,primary_entity_id,((payload->'due_date'->>'to_revision')::integer))
    WHERE event_type='ticket.due-date-changed';

UPDATE radishnexus.activity_items SET projection_version=14 WHERE projection_version=13;
---- create above / drop below ----
-- Forward-only; restore a matching artifact and backup into an empty target.
