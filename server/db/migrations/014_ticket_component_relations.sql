INSERT INTO radishnexus.relation_types(from_type,relation_type,to_type) VALUES('ticket','affects','component');
ALTER TABLE radishnexus.entity_links ADD CONSTRAINT ticket_component_provenance CHECK (
    NOT (from_type='ticket' AND relation_type='affects' AND to_type='component') OR (
        assertion='asserted' AND origin='user' AND origin_ref IS NULL AND created_by_kind='user'
        AND source_event_id IS NULL AND metadata='{}'::jsonb
        AND (state='active' OR (removed_by_kind='user' AND removal_reason='ticket-component-unlinked'))
    )
);
CREATE UNIQUE INDEX ticket_component_active ON radishnexus.entity_links(workspace_id,from_id,to_id)
WHERE from_type='ticket' AND relation_type='affects' AND to_type='component' AND state='active';
CREATE INDEX component_tickets_active ON radishnexus.entity_links(workspace_id,to_id,from_id)
WHERE from_type='ticket' AND relation_type='affects' AND to_type='component' AND state='active';

ALTER TABLE radishnexus.collaboration_command_receipts
    DROP CONSTRAINT collaboration_command_receipts_kind,
    DROP CONSTRAINT collaboration_command_receipts_target,
    DROP CONSTRAINT collaboration_command_receipts_result,
    ADD CONSTRAINT collaboration_command_receipts_kind CHECK (command_kind IN ('decision.propose','decision.accept','ticket.create','document.create','document.save','document.restore','deployment.record','ticket.component.link','ticket.component.unlink')),
    ADD CONSTRAINT collaboration_command_receipts_target CHECK (
        (command_kind='decision.propose' AND target_type='thread') OR
        (command_kind IN ('decision.accept','ticket.create') AND target_type='decision') OR
        (command_kind='document.create' AND target_type='ticket') OR
        (command_kind IN ('document.save','document.restore') AND target_type='document') OR
        (command_kind='deployment.record' AND target_type='ci-run') OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink') AND target_type='ticket')),
    ADD CONSTRAINT collaboration_command_receipts_result CHECK (
        (command_kind IN ('decision.propose','decision.accept') AND result_type='decision' AND result_revision IS NULL) OR
        (command_kind='ticket.create' AND result_type='ticket' AND result_revision IS NULL) OR
        (command_kind IN ('document.create','document.save','document.restore') AND result_type='document' AND result_revision IS NOT NULL AND result_revision>0) OR
        (command_kind='deployment.record' AND result_type='deployment' AND result_revision IS NULL) OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink') AND result_type='entity-link' AND result_revision IS NULL));

CREATE FUNCTION radishnexus.validate_ticket_component_receipt() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF NEW.command_kind IN ('ticket.component.link','ticket.component.unlink') AND NOT EXISTS (
        SELECT 1 FROM entity_links l
        JOIN tickets t ON t.workspace_id=l.workspace_id AND t.id=l.from_id
        JOIN domain_events e ON e.workspace_id=l.workspace_id AND e.event_id=NEW.event_id
        WHERE l.workspace_id=NEW.workspace_id AND l.id=NEW.result_id
        AND l.from_type='ticket' AND l.from_id=NEW.target_id
        AND l.relation_type='affects' AND l.to_type='component'
        AND e.primary_entity_type='ticket' AND e.primary_entity_id=t.id
        AND e.project_id=t.governing_project_id
        AND e.actor_kind='user' AND e.actor_id=NEW.actor_id AND e.source_kind='web'
        AND e.schema_version=1 AND e.occurred_at=NEW.created_at
        AND e.event_type=CASE WHEN NEW.command_kind='ticket.component.link' THEN 'ticket.component-linked' ELSE 'ticket.component-unlinked' END
        AND e.payload=jsonb_build_object('component',jsonb_build_object('type','component','id',l.to_id),'link_id',l.id,'state',CASE WHEN NEW.command_kind='ticket.component.link' THEN 'active' ELSE 'removed' END)
        AND ((NEW.command_kind='ticket.component.link' AND l.state='active' AND l.created_by_id=NEW.actor_id AND l.created_at=NEW.created_at)
            OR (NEW.command_kind='ticket.component.unlink' AND l.state='removed' AND l.removed_by_id=NEW.actor_id AND l.removed_at=NEW.created_at))
    ) THEN RAISE EXCEPTION 'Ticket Component receipt must identify the exact relation operation' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER ticket_component_receipt_source AFTER INSERT ON radishnexus.collaboration_command_receipts
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_ticket_component_receipt();

UPDATE radishnexus.activity_items SET projection_version=5 WHERE projection_version=4;

---- create above / drop below ----
-- Forward-only. Restore the pre-upgrade backup to a new target for version rollback.
