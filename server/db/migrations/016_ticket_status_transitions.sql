-- A pre-workflow instance cannot have legitimate non-open Ticket history.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM radishnexus.tickets WHERE status <> 'open') THEN
        RAISE EXCEPTION 'Ticket status migration requires an open-only baseline; investigate unsupported state before upgrading';
    END IF;
END $$;
ALTER TABLE radishnexus.tickets ADD COLUMN status_revision integer NOT NULL DEFAULT 1
    CONSTRAINT tickets_status_revision_positive CHECK (status_revision > 0);

ALTER TABLE radishnexus.collaboration_command_receipts
    DROP CONSTRAINT collaboration_command_receipts_kind,
    DROP CONSTRAINT collaboration_command_receipts_target,
    DROP CONSTRAINT collaboration_command_receipts_result,
    ADD CONSTRAINT collaboration_command_receipts_kind CHECK (command_kind IN ('decision.propose','decision.accept','ticket.create','document.create','document.save','document.restore','deployment.record','ticket.component.link','ticket.component.unlink','ticket.status.change')),
    ADD CONSTRAINT collaboration_command_receipts_target CHECK (
        (command_kind='decision.propose' AND target_type='thread') OR
        (command_kind IN ('decision.accept','ticket.create') AND target_type='decision') OR
        (command_kind='document.create' AND target_type='ticket') OR
        (command_kind IN ('document.save','document.restore') AND target_type='document') OR
        (command_kind='deployment.record' AND target_type='ci-run') OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink','ticket.status.change') AND target_type='ticket')),
    ADD CONSTRAINT collaboration_command_receipts_result CHECK (
        (command_kind IN ('decision.propose','decision.accept') AND result_type='decision' AND result_revision IS NULL) OR
        (command_kind='ticket.create' AND result_type='ticket' AND result_revision IS NULL) OR
        (command_kind IN ('document.create','document.save','document.restore') AND result_type='document' AND result_revision IS NOT NULL AND result_revision>0) OR
        (command_kind='deployment.record' AND result_type='deployment' AND result_revision IS NULL) OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink') AND result_type='entity-link' AND result_revision IS NULL) OR
        (command_kind='ticket.status.change' AND result_type='ticket' AND result_id=target_id AND result_revision IS NOT NULL AND result_revision>1));

CREATE FUNCTION radishnexus.valid_ticket_status_transition(before_status text, after_status text)
RETURNS boolean LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT before_status IN ('open','in-progress','done','canceled')
       AND after_status IN ('open','in-progress','done','canceled')
       AND before_status <> after_status
       AND (before_status IN ('open','in-progress') OR after_status='open')
$$;

CREATE FUNCTION radishnexus.validate_ticket_status_receipt() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF NEW.command_kind='ticket.status.change' AND NOT EXISTS (
        SELECT 1 FROM domain_events e
        JOIN tickets t ON t.workspace_id=e.workspace_id AND t.id=e.primary_entity_id
        WHERE e.workspace_id=NEW.workspace_id AND e.event_id=NEW.event_id
        AND e.primary_entity_type='ticket' AND e.primary_entity_id=NEW.target_id
        AND NEW.result_type='ticket' AND NEW.result_id=NEW.target_id
        AND NEW.result_revision>1 AND t.status_revision>=NEW.result_revision
        AND e.project_id=t.governing_project_id
        AND e.actor_kind='user' AND e.actor_id=NEW.actor_id AND e.source_kind='web'
        AND e.schema_version=1 AND e.event_type='ticket.status-changed' AND e.occurred_at=NEW.created_at
        AND valid_ticket_status_transition(e.payload->>'from_status',e.payload->>'to_status')
        AND e.payload=jsonb_build_object(
            'from_status',e.payload->>'from_status','to_status',e.payload->>'to_status',
            'from_status_revision',NEW.result_revision-1,'to_status_revision',NEW.result_revision)
    ) THEN RAISE EXCEPTION 'Ticket status receipt must identify the exact state transition' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER ticket_status_receipt_source AFTER INSERT ON radishnexus.collaboration_command_receipts
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_ticket_status_receipt();

CREATE FUNCTION radishnexus.validate_ticket_status_event() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM collaboration_command_receipts r
        WHERE r.workspace_id=NEW.workspace_id AND r.event_id=NEW.event_id
        AND r.command_kind='ticket.status.change') THEN
        RAISE EXCEPTION 'Ticket state transition must have its command receipt' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER ticket_status_event_receipt AFTER INSERT ON radishnexus.domain_events
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.event_type='ticket.status-changed')
EXECUTE FUNCTION radishnexus.validate_ticket_status_event();
CREATE UNIQUE INDEX ticket_status_event_revision ON radishnexus.domain_events
    (workspace_id,primary_entity_id,(payload->>'to_status_revision')) WHERE event_type='ticket.status-changed';

UPDATE radishnexus.activity_items SET projection_version=7 WHERE projection_version=6;

---- create above / drop below ----
-- Forward-only. Restore the pre-upgrade backup to a new target for version rollback.
