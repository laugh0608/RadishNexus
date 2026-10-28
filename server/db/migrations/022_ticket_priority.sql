-- ADR-0047: explicit priority is independent of status and responsibility.
ALTER TABLE radishnexus.tickets
 ADD COLUMN priority text NOT NULL DEFAULT 'unset' CHECK(priority IN ('unset','low','normal','high','urgent')),
 ADD COLUMN priority_revision integer NOT NULL DEFAULT 1 CHECK(priority_revision>0);

ALTER TABLE radishnexus.collaboration_command_receipts
    DROP CONSTRAINT collaboration_command_receipts_kind,
    DROP CONSTRAINT collaboration_command_receipts_target,
    DROP CONSTRAINT collaboration_command_receipts_result,
    ADD CONSTRAINT collaboration_command_receipts_kind CHECK (command_kind IN ('decision.propose','decision.accept','ticket.create','document.create','document.save','document.restore','deployment.record','ticket.component.link','ticket.component.unlink','ticket.status.change','ticket.assignee.set','ticket.priority.set')),
    ADD CONSTRAINT collaboration_command_receipts_target CHECK (
        (command_kind='decision.propose' AND target_type='thread') OR
        (command_kind IN ('decision.accept','ticket.create') AND target_type='decision') OR
        (command_kind='document.create' AND target_type='ticket') OR
        (command_kind IN ('document.save','document.restore') AND target_type='document') OR
        (command_kind='deployment.record' AND target_type='ci-run') OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink','ticket.status.change','ticket.assignee.set','ticket.priority.set') AND target_type='ticket')),
    ADD CONSTRAINT collaboration_command_receipts_result CHECK (
        (command_kind IN ('decision.propose','decision.accept') AND result_type='decision' AND result_revision IS NULL) OR
        (command_kind='ticket.create' AND result_type='ticket' AND result_revision IS NULL) OR
        (command_kind IN ('document.create','document.save','document.restore') AND result_type='document' AND result_revision IS NOT NULL AND result_revision>0) OR
        (command_kind='deployment.record' AND result_type='deployment' AND result_revision IS NULL) OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink') AND result_type='entity-link' AND result_revision IS NULL) OR
        (command_kind IN ('ticket.status.change','ticket.assignee.set','ticket.priority.set') AND result_type='ticket' AND result_id=target_id AND result_revision IS NOT NULL AND result_revision>1));


CREATE FUNCTION radishnexus.valid_ticket_priority(f jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE p jsonb:=f->'priority'; before_revision integer; after_revision integer;
BEGIN
 IF jsonb_typeof(p) IS DISTINCT FROM 'object' THEN RETURN false; END IF;
 before_revision:=(p->>'from_revision')::integer; after_revision:=(p->>'to_revision')::integer;
 RETURN COALESCE(f=jsonb_build_object('priority',jsonb_build_object(
  'from',p->>'from','to',p->>'to','from_revision',before_revision,'to_revision',after_revision))
  AND p->>'from' IN ('unset','low','normal','high','urgent')
  AND p->>'to' IN ('unset','low','normal','high','urgent')
  AND p->>'from'<>p->>'to'
  AND before_revision>0 AND before_revision<2147483647 AND after_revision=before_revision+1,false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END; $$;

CREATE FUNCTION radishnexus.validate_ticket_priority_receipt() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
 IF NEW.command_kind='ticket.priority.set' AND NOT EXISTS(
 SELECT 1 FROM domain_events e JOIN tickets t ON t.workspace_id=e.workspace_id AND t.id=e.primary_entity_id
 WHERE e.workspace_id=NEW.workspace_id AND e.event_id=NEW.event_id
 AND e.primary_entity_type='ticket' AND e.primary_entity_id=NEW.target_id
 AND e.event_type='ticket.priority-changed' AND e.schema_version=1 AND e.project_id=t.governing_project_id
 AND e.actor_kind='user' AND e.actor_id=NEW.actor_id AND e.source_kind='web' AND e.occurred_at=NEW.created_at
 AND NEW.result_type='ticket' AND NEW.result_id=NEW.target_id AND t.priority_revision>=NEW.result_revision
 AND valid_ticket_priority(e.payload) AND (e.payload->'priority'->>'to_revision')::integer=NEW.result_revision
 ) THEN RAISE EXCEPTION 'Ticket priority receipt requires exact event' USING ERRCODE='23514'; END IF;
 RETURN NULL;
END; $$;
CREATE CONSTRAINT TRIGGER ticket_priority_receipt AFTER INSERT ON radishnexus.collaboration_command_receipts
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.validate_ticket_priority_receipt();
CREATE FUNCTION radishnexus.validate_ticket_priority_event() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
 IF NOT valid_ticket_priority(NEW.payload) OR NOT EXISTS(
 SELECT 1 FROM collaboration_command_receipts r WHERE r.workspace_id=NEW.workspace_id AND r.event_id=NEW.event_id AND r.command_kind='ticket.priority.set'
 ) THEN RAISE EXCEPTION 'Ticket priority event requires receipt' USING ERRCODE='23514'; END IF;
 RETURN NULL;
END; $$;
CREATE CONSTRAINT TRIGGER ticket_priority_event AFTER INSERT ON radishnexus.domain_events
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN(NEW.event_type='ticket.priority-changed') EXECUTE FUNCTION radishnexus.validate_ticket_priority_event();
CREATE UNIQUE INDEX ticket_priority_event_revision ON radishnexus.domain_events
 (workspace_id,primary_entity_id,((payload->'priority'->>'to_revision')::integer)) WHERE event_type='ticket.priority-changed';

ALTER TABLE radishnexus.context_import_history DROP CONSTRAINT context_import_history_original_type_check,
 ADD CONSTRAINT context_import_history_original_type_check CHECK(original_type IN ('project.created','channel.created','message.created','thread.started','decision.proposed','decision.accepted','ticket.created','ticket.status-changed','ticket.assignee-changed','ticket.priority-changed','document.created','document.revised'));
CREATE OR REPLACE FUNCTION radishnexus.validate_context_facts() RETURNS trigger
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
      WHEN 'ticket.priority-changed' THEN
        IF NOT valid_ticket_priority(f) THEN RAISE EXCEPTION 'Invalid historical priority' USING ERRCODE='23514'; END IF;
        expected:=f;
      WHEN 'ticket.assignee-changed' THEN
        IF NOT valid_ticket_assignment(f) THEN RAISE EXCEPTION 'Invalid historical assignment' USING ERRCODE='23514'; END IF;
        FOR pair IN SELECT value FROM jsonb_array_elements(jsonb_build_array(f->'assignment'->'from_user_id',f->'assignment'->'to_user_id')) LOOP
          IF pair.value<>'null'::jsonb AND NOT EXISTS(SELECT 1 FROM context_import_mappings WHERE workspace_id=NEW.workspace_id AND operation_id=NEW.operation_id AND kind='actor' AND target_id=pair.value#>>'{}') THEN RAISE EXCEPTION 'Invalid historical assignee' USING ERRCODE='23514'; END IF;
        END LOOP;
        expected:=f;
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

UPDATE radishnexus.activity_items SET projection_version=10 WHERE projection_version=9;

---- create above / drop below ----
-- Forward-only; recover the matching old artifact and backup into an empty target.
