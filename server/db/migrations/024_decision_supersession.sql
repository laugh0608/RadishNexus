-- ADR-0049: one immutable supersession advances both Decision revisions.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM radishnexus.decisions WHERE status NOT IN ('proposed','accepted','rejected') OR (status='accepted' AND (decided_at<created_at OR decided_at<>updated_at))) OR
    EXISTS(SELECT 1 FROM radishnexus.entity_links WHERE relation_type='supersedes')
 THEN RAISE EXCEPTION 'Supersession migration requires the supported three-state baseline' USING ERRCODE='23514'; END IF;
END $$;
DROP TRIGGER rejected_decision_immutable ON radishnexus.decisions;
ALTER TABLE radishnexus.decisions ADD COLUMN decision_revision integer NOT NULL DEFAULT 1 CHECK(decision_revision>0);
UPDATE radishnexus.decisions SET decision_revision=2 WHERE status IN ('accepted','rejected');
CREATE TRIGGER rejected_decision_immutable BEFORE UPDATE ON radishnexus.decisions FOR EACH ROW EXECUTE FUNCTION radishnexus.guard_rejected_decision();
ALTER TABLE radishnexus.collaboration_command_receipts
    DROP CONSTRAINT collaboration_command_receipts_kind,
    DROP CONSTRAINT collaboration_command_receipts_target,
    DROP CONSTRAINT collaboration_command_receipts_result,
    ADD CONSTRAINT collaboration_command_receipts_kind CHECK (command_kind IN ('decision.propose','decision.accept','decision.reject','decision.supersede','ticket.create','document.create','document.save','document.restore','deployment.record','ticket.component.link','ticket.component.unlink','ticket.status.change','ticket.assignee.set','ticket.priority.set')),
    ADD CONSTRAINT collaboration_command_receipts_target CHECK (
        (command_kind='decision.propose' AND target_type='thread') OR
        (command_kind IN ('decision.accept','decision.reject','decision.supersede','ticket.create') AND target_type='decision') OR
        (command_kind='document.create' AND target_type='ticket') OR
        (command_kind IN ('document.save','document.restore') AND target_type='document') OR
        (command_kind='deployment.record' AND target_type='ci-run') OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink','ticket.status.change','ticket.assignee.set','ticket.priority.set') AND target_type='ticket')),
    ADD CONSTRAINT collaboration_command_receipts_result CHECK (
        (command_kind IN ('decision.propose','decision.accept','decision.reject') AND result_type='decision' AND result_revision IS NULL) OR
        (command_kind='ticket.create' AND result_type='ticket' AND result_revision IS NULL) OR
        (command_kind IN ('document.create','document.save','document.restore') AND result_type='document' AND result_revision IS NOT NULL AND result_revision>0) OR
        (command_kind='deployment.record' AND result_type='deployment' AND result_revision IS NULL) OR
        (command_kind IN ('ticket.component.link','ticket.component.unlink','decision.supersede') AND result_type='entity-link' AND result_revision IS NULL) OR
        (command_kind IN ('ticket.status.change','ticket.assignee.set','ticket.priority.set') AND result_type='ticket' AND result_id=target_id AND result_revision IS NOT NULL AND result_revision>1));

CREATE FUNCTION radishnexus.valid_decision_supersession(f jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$ SELECT
 jsonb_typeof(f)='object' AND f ?& ARRAY['status','replacement','relation','from_revision','to_revision','replacement_from_revision','replacement_to_revision']
 AND f - ARRAY['status','replacement','relation','from_revision','to_revision','replacement_from_revision','replacement_to_revision']='{}'::jsonb
 AND f->>'status'='superseded'
 AND f->'replacement'=jsonb_build_object('type','decision','id',f->'replacement'->>'id')
 AND radishnexus.valid_entity_id('decision',f->'replacement'->>'id')
 AND f->'relation'=jsonb_build_object('type','entity-link','id',f->'relation'->>'id')
 AND radishnexus.valid_entity_id('entity-link',f->'relation'->>'id')
 AND f->'from_revision' IN ('2'::jsonb,'3'::jsonb)
 AND f->'to_revision'=to_jsonb((f->>'from_revision')::integer+1)
 AND f->'replacement_from_revision'='2'::jsonb AND f->'replacement_to_revision'='3'::jsonb $$;

CREATE UNIQUE INDEX decision_supersedes_one_predecessor ON radishnexus.entity_links(workspace_id,from_id) WHERE relation_type='supersedes';
CREATE UNIQUE INDEX decision_supersedes_one_successor ON radishnexus.entity_links(workspace_id,to_id) WHERE relation_type='supersedes';
CREATE FUNCTION radishnexus.valid_supersession_reason(value text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
 SELECT value<>'' AND octet_length(value)<=32768 AND value=btrim(value,chr(9)||chr(10)||chr(11)||chr(12)||chr(13)||chr(32)||chr(133)||chr(160)||chr(5760)||chr(8192)||chr(8193)||chr(8194)||chr(8195)||chr(8196)||chr(8197)||chr(8198)||chr(8199)||chr(8200)||chr(8201)||chr(8202)||chr(8232)||chr(8233)||chr(8239)||chr(8287)||chr(12288))
$$;

CREATE FUNCTION radishnexus.guard_decision_supersession_link() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN
  IF OLD.relation_type='supersedes' AND (TG_OP='DELETE' OR NEW IS DISTINCT FROM OLD) THEN RAISE EXCEPTION 'Supersession is immutable' USING ERRCODE='23514'; END IF;
  IF TG_OP='DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
 END IF;
 IF NEW.relation_type='supersedes' THEN
  IF NEW.from_type<>'decision' OR NEW.to_type<>'decision' OR NEW.from_id=NEW.to_id OR NEW.state<>'active'
     OR NEW.assertion<>'asserted' OR NEW.origin NOT IN ('user','import') OR NEW.created_at<>NEW.updated_at
     OR jsonb_typeof(NEW.metadata->'supersession_reason') IS DISTINCT FROM 'string'
     OR NEW.metadata<>jsonb_build_object('supersession_reason',NEW.metadata->>'supersession_reason')
     OR NOT valid_supersession_reason(NEW.metadata->>'supersession_reason')
     OR NOT EXISTS(SELECT 1 FROM decisions a JOIN decisions b ON a.workspace_id=b.workspace_id AND a.governing_project_id=b.governing_project_id
        WHERE a.workspace_id=NEW.workspace_id AND a.id=NEW.from_id AND b.id=NEW.to_id)
  THEN RAISE EXCEPTION 'Invalid supersession relation' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER decision_supersession_link_guard BEFORE INSERT OR UPDATE OR DELETE ON radishnexus.entity_links FOR EACH ROW EXECUTE FUNCTION radishnexus.guard_decision_supersession_link();

CREATE FUNCTION radishnexus.guard_decision_lifecycle() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
 IF NEW IS NOT DISTINCT FROM OLD THEN RETURN NEW; END IF;
 IF NEW.question<>OLD.question OR NEW.proposer_id<>OLD.proposer_id OR NEW.created_at<>OLD.created_at
    OR NEW.created_by_kind<>OLD.created_by_kind OR NEW.created_by_id IS DISTINCT FROM OLD.created_by_id OR NEW.alternatives<>OLD.alternatives OR NEW.consequences IS DISTINCT FROM OLD.consequences OR NEW.review_at IS DISTINCT FROM OLD.review_at
    OR NEW.decision_revision<>OLD.decision_revision+1 OR NEW.updated_at<=OLD.updated_at
    OR NOT ((OLD.status='proposed' AND NEW.status IN ('accepted','rejected')) OR (OLD.status='accepted' AND NEW.status IN ('accepted','superseded')))
 THEN RAISE EXCEPTION 'Invalid immutable Decision lifecycle change' USING ERRCODE='23514'; END IF;
 IF OLD.status='accepted' AND (NEW.outcome IS DISTINCT FROM OLD.outcome OR NEW.rationale IS DISTINCT FROM OLD.rationale
    OR NEW.rejection_reason IS DISTINCT FROM OLD.rejection_reason OR NEW.decider_ids<>OLD.decider_ids OR NEW.decided_at IS DISTINCT FROM OLD.decided_at)
 THEN RAISE EXCEPTION 'Accepted Decision facts are immutable' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER decision_lifecycle_guard BEFORE UPDATE ON radishnexus.decisions FOR EACH ROW EXECUTE FUNCTION radishnexus.guard_decision_lifecycle();

CREATE FUNCTION radishnexus.validate_decision_supersession_link(w text, link text) RETURNS void
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE l entity_links%ROWTYPE; old_dec decisions%ROWTYPE; new_dec decisions%ROWTYPE; f jsonb; typ text; actor text; at_time timestamptz; imported boolean; base integer; link_actor text;
BEGIN
 SELECT * INTO l FROM entity_links WHERE workspace_id=w AND id=link AND relation_type='supersedes';
 IF NOT FOUND THEN RAISE EXCEPTION 'Supersession relation required' USING ERRCODE='23514'; END IF;
 SELECT * INTO old_dec FROM decisions WHERE workspace_id=w AND id=l.to_id;
 SELECT * INTO new_dec FROM decisions WHERE workspace_id=w AND id=l.from_id;
 SELECT COALESCE(h.original_facts,e.payload),COALESCE(h.original_type,e.event_type),COALESCE(h.original_actor,e.actor_id),COALESCE(h.original_time,e.occurred_at),h.event_id IS NOT NULL
 INTO f,typ,actor,at_time,imported FROM domain_events e LEFT JOIN context_import_history h ON h.workspace_id=e.workspace_id AND h.event_id=e.event_id
 WHERE e.workspace_id=w AND e.event_id=l.source_event_id AND e.primary_entity_type='decision' AND e.primary_entity_id=l.to_id AND e.project_id=old_dec.governing_project_id AND e.schema_version=1;
 link_actor:=l.created_by_id; IF l.origin='import' THEN SELECT source_actor_id INTO link_actor FROM context_import_mappings WHERE workspace_id=w AND kind='entity-link' AND target_id=l.id; END IF;
 base:=2+(SELECT count(*)::integer FROM entity_links WHERE workspace_id=w AND from_id=l.to_id AND relation_type='supersedes' AND created_at<l.created_at);
 IF f IS NULL OR typ<>'decision.superseded' OR NOT valid_decision_supersession(f)
    OR f->'replacement'<>jsonb_build_object('type','decision','id',l.from_id) OR f->'relation'<>jsonb_build_object('type','entity-link','id',l.id)
    OR f->'from_revision'<>to_jsonb(base) OR actor IS DISTINCT FROM link_actor OR at_time<>l.created_at
    OR old_dec.status<>'superseded' OR new_dec.status NOT IN ('accepted','superseded') OR l.created_at<=old_dec.decided_at OR l.created_at<=new_dec.decided_at
    OR imported<>(l.origin='import')
 THEN RAISE EXCEPTION 'Supersession provenance mismatch' USING ERRCODE='23514'; END IF;
 IF NOT imported AND NOT EXISTS(SELECT 1 FROM collaboration_command_receipts r JOIN domain_events e ON e.workspace_id=r.workspace_id AND e.event_id=r.event_id
    WHERE r.workspace_id=w AND r.command_kind='decision.supersede' AND r.target_type='decision' AND r.target_id=l.to_id AND r.result_type='entity-link'
    AND r.result_id=l.id AND r.result_revision IS NULL AND r.actor_id=actor AND r.created_at=at_time AND r.event_id=l.source_event_id AND e.actor_kind='user' AND e.source_kind='web')
 THEN RAISE EXCEPTION 'Native supersession requires exact receipt' USING ERRCODE='23514'; END IF;
 IF EXISTS(WITH RECURSIVE reach(id) AS(SELECT l.to_id UNION SELECT e.to_id FROM entity_links e JOIN reach r ON e.from_id=r.id WHERE e.workspace_id=w AND e.relation_type='supersedes') SELECT 1 FROM reach WHERE id=l.from_id)
 THEN RAISE EXCEPTION 'Supersession cycle' USING ERRCODE='23514'; END IF;
END $$;

CREATE FUNCTION radishnexus.validate_decision_lifecycle(w text, target text) RETURNS void
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE d decisions%ROWTYPE; outs integer; ins integer; last_time timestamptz; l record;
BEGIN
 SELECT * INTO d FROM decisions WHERE workspace_id=w AND id=target;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT count(*) FILTER(WHERE from_id=target),count(*) FILTER(WHERE to_id=target),max(created_at) INTO outs,ins,last_time FROM entity_links WHERE workspace_id=w AND relation_type='supersedes' AND (from_id=target OR to_id=target);
 IF (d.status='proposed' AND (d.decision_revision<>1 OR outs<>0 OR ins<>0))
    OR (d.status='rejected' AND (d.decision_revision<>2 OR outs<>0 OR ins<>0 OR d.updated_at<>d.decided_at))
    OR (d.status='accepted' AND (ins<>0 OR d.decision_revision<>2+outs OR d.updated_at<>GREATEST(d.decided_at,last_time)))
    OR (d.status='superseded' AND (ins<>1 OR d.decision_revision<>2+outs+ins OR d.updated_at<>GREATEST(d.decided_at,last_time)))
 THEN RAISE EXCEPTION 'Decision revision or history mismatch' USING ERRCODE='23514'; END IF;
 FOR l IN SELECT id FROM entity_links WHERE workspace_id=w AND relation_type='supersedes' AND (from_id=target OR to_id=target) LOOP
  PERFORM validate_decision_supersession_link(w,l.id);
 END LOOP;
END $$;
CREATE FUNCTION radishnexus.check_decision_lifecycle() RETURNS trigger LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$ BEGIN PERFORM validate_decision_lifecycle(NEW.workspace_id,NEW.id); RETURN NULL; END $$;
CREATE CONSTRAINT TRIGGER decision_lifecycle_consistent AFTER INSERT OR UPDATE ON radishnexus.decisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.check_decision_lifecycle();
CREATE FUNCTION radishnexus.check_supersession_link() RETURNS trigger LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$ BEGIN IF NEW.relation_type='supersedes' THEN PERFORM validate_decision_lifecycle(NEW.workspace_id,NEW.from_id); PERFORM validate_decision_lifecycle(NEW.workspace_id,NEW.to_id); END IF; RETURN NULL; END $$;
CREATE CONSTRAINT TRIGGER supersession_link_consistent AFTER INSERT ON radishnexus.entity_links DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.check_supersession_link();
CREATE FUNCTION radishnexus.check_supersession_event() RETURNS trigger LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE f jsonb; link text;
BEGIN
 IF TG_TABLE_NAME='context_import_history' THEN f:=NEW.original_facts; ELSE f:=NEW.payload; END IF;
 link:=f->'relation'->>'id';
 PERFORM validate_decision_supersession_link(NEW.workspace_id,link);
 IF NOT EXISTS(SELECT 1 FROM entity_links WHERE workspace_id=NEW.workspace_id AND id=link AND source_event_id=NEW.event_id) THEN RAISE EXCEPTION 'Supersession event relation mismatch' USING ERRCODE='23514'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER supersession_event_consistent AFTER INSERT ON radishnexus.domain_events DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN(NEW.event_type='decision.superseded') EXECUTE FUNCTION radishnexus.check_supersession_event();
CREATE CONSTRAINT TRIGGER supersession_history_consistent AFTER INSERT ON radishnexus.context_import_history DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN(NEW.original_type='decision.superseded') EXECUTE FUNCTION radishnexus.check_supersession_event();
CREATE FUNCTION radishnexus.check_supersession_receipt() RETURNS trigger LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$ BEGIN IF NEW.command_kind='decision.supersede' THEN PERFORM validate_decision_supersession_link(NEW.workspace_id,NEW.result_id); END IF; RETURN NULL; END $$;
CREATE CONSTRAINT TRIGGER supersession_receipt_consistent AFTER INSERT ON radishnexus.collaboration_command_receipts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.check_supersession_receipt();

ALTER TABLE radishnexus.context_import_history DROP CONSTRAINT context_import_history_original_type_check,
 ADD CONSTRAINT context_import_history_original_type_check CHECK(original_type IN ('project.created','channel.created','message.created','thread.started','decision.proposed','decision.accepted','decision.rejected','decision.superseded','ticket.created','ticket.status-changed','ticket.assignee-changed','ticket.priority-changed','document.created','document.revised'));


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
      WHEN 'decision.rejected' THEN expected:=jsonb_build_object('status','rejected');
      WHEN 'decision.superseded' THEN
        IF NOT valid_decision_supersession(f) THEN RAISE EXCEPTION 'Invalid supersession facts' USING ERRCODE='23514'; END IF;
        expected:=f;
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
    FOR pair IN SELECT key,value FROM jsonb_each(f) WHERE key IN ('channel','thread','message','evidence','decision','ticket','replacement','relation') LOOP
        expected_kind:=CASE WHEN pair.key='evidence' THEN 'thread' WHEN pair.key='replacement' THEN 'decision' WHEN pair.key='relation' THEN 'entity-link' ELSE pair.key END;
        IF pair.value IS DISTINCT FROM jsonb_build_object('type',expected_kind,'id',pair.value->>'id') OR NOT EXISTS (
            SELECT 1 FROM context_import_mappings WHERE workspace_id=NEW.workspace_id AND operation_id=NEW.operation_id
            AND kind=expected_kind AND target_id=pair.value->>'id'
        ) THEN RAISE EXCEPTION 'Invalid historical reference' USING ERRCODE='23514'; END IF;
    END LOOP;
    RETURN NULL;
END;
$$;


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
            OR (NEW.from_type='ticket' AND NEW.relation_type='relates-to' AND NEW.to_type='document' AND h.original_type='document.created'
                AND e.primary_entity_type='document' AND e.primary_entity_id=NEW.to_id AND h.original_facts->'ticket'=jsonb_build_object('type','ticket','id',NEW.from_id))
        )
    ) THEN RAISE EXCEPTION 'Imported link requires exact provenance' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;

UPDATE radishnexus.activity_items SET projection_version=12 WHERE projection_version=11;
---- create above / drop below ----
-- Forward-only; restore a matching artifact and backup into an empty target.
