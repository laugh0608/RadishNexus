CREATE TABLE radishnexus.ci_source_checkout_bindings (
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces(id),
    source_kind text NOT NULL CHECK (source_kind='jenkins'),
    source_id text NOT NULL CHECK (source_id ~ '^[A-Za-z][A-Za-z0-9_-]{0,63}$'),
    component_id text NOT NULL,
    job_full_name text NOT NULL CHECK (job_full_name<>'' AND btrim(job_full_name)=job_full_name AND octet_length(job_full_name)<=255 AND job_full_name !~ '[[:cntrl:]]'),
    binding_id text NOT NULL CHECK (binding_id ~ '^[A-Za-z][A-Za-z0-9_-]{0,63}$'),
    repository_id text NOT NULL,
    provider text NOT NULL CHECK (provider IN ('github','gitlab','gitea')),
    provider_origin text NOT NULL,
    external_id text NOT NULL,
    profile text NOT NULL CHECK (profile='jenkins-git-single-checkout-v1'),
    effective_from_build bigint NOT NULL CHECK (effective_from_build BETWEEN 1 AND 2147483647),
    recorded_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id,source_kind,source_id),
    UNIQUE (workspace_id,source_kind,source_id,binding_id),
    FOREIGN KEY (workspace_id,component_id) REFERENCES radishnexus.components(workspace_id,id),
    FOREIGN KEY (workspace_id,repository_id) REFERENCES radishnexus.repositories(workspace_id,id)
);

CREATE FUNCTION radishnexus.protect_ci_checkout_binding() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'CI checkout bindings are immutable' USING ERRCODE='23514';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM repositories r WHERE r.workspace_id=NEW.workspace_id AND r.id=NEW.repository_id
        AND r.provider=NEW.provider AND r.provider_origin=NEW.provider_origin AND r.external_id=NEW.external_id) THEN
        RAISE EXCEPTION 'CI checkout Repository identity mismatch' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER ci_checkout_binding_identity BEFORE INSERT OR UPDATE OR DELETE ON radishnexus.ci_source_checkout_bindings
FOR EACH ROW EXECUTE FUNCTION radishnexus.protect_ci_checkout_binding();

ALTER TABLE radishnexus.ci_runs
    ADD COLUMN checkout_state text NOT NULL DEFAULT 'not_collected',
    ADD COLUMN checkout_reason text,
    ADD COLUMN checkout_binding_id text,
    ADD CONSTRAINT ci_runs_checkout_shape CHECK (
        (checkout_state='not_collected' AND checkout_reason IS NULL AND checkout_binding_id IS NULL) OR
        (checkout_state='observed' AND checkout_reason IS NULL AND checkout_binding_id IS NOT NULL) OR
        (checkout_state='unavailable' AND checkout_reason IS NOT NULL AND checkout_reason IN
            ('checkout_not_observed','unsupported_checkout','binding_changed_during_run') AND checkout_binding_id IS NOT NULL)),
    ADD CONSTRAINT ci_runs_checkout_binding FOREIGN KEY (workspace_id,source_kind,source_id,checkout_binding_id)
        REFERENCES radishnexus.ci_source_checkout_bindings(workspace_id,source_kind,source_id,binding_id);

CREATE FUNCTION radishnexus.protect_ci_checkout_fact() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        IF OLD.checkout_state<>'not_collected' THEN
            RAISE EXCEPTION 'CI checkout facts cannot be deleted' USING ERRCODE='23514';
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.checkout_state<>OLD.checkout_state OR NEW.checkout_reason IS DISTINCT FROM OLD.checkout_reason
       OR NEW.checkout_binding_id IS DISTINCT FROM OLD.checkout_binding_id
       OR (OLD.checkout_state<>'not_collected' AND NEW IS DISTINCT FROM OLD) THEN
        RAISE EXCEPTION 'CI checkout facts are immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER ci_checkout_fact_immutable BEFORE UPDATE OR DELETE ON radishnexus.ci_runs
FOR EACH ROW EXECUTE FUNCTION radishnexus.protect_ci_checkout_fact();

INSERT INTO radishnexus.relation_types(from_type,relation_type,to_type) VALUES('ci-run','built-from','repository');
ALTER TABLE radishnexus.entity_links ADD CONSTRAINT ci_checkout_link_shape CHECK (
    relation_type<>'built-from' OR (
        from_type='ci-run' AND to_type='repository' AND assertion='asserted' AND origin='plugin'
        AND created_by_kind='plugin' AND created_by_id IS NOT NULL
        AND origin_ref IS NOT NULL AND source_event_id IS NOT NULL AND state='active'
        AND metadata ?& ARRAY['schema_version','profile','binding_id','algorithm','oid']
        AND metadata - ARRAY['schema_version','profile','binding_id','algorithm','oid']='{}'::jsonb
        AND metadata->'schema_version'='1'::jsonb
        AND metadata->>'profile'='jenkins-git-single-checkout-v1'
        AND metadata->>'binding_id' ~ '^[A-Za-z][A-Za-z0-9_-]{0,63}$'
        AND metadata->>'algorithm'='sha1'
        AND metadata->>'oid' ~ '^[0-9a-f]{40}$'
        AND metadata->>'oid'<>repeat('0',40)
        AND jsonb_typeof(metadata->'profile')='string' AND jsonb_typeof(metadata->'binding_id')='string'
        AND jsonb_typeof(metadata->'algorithm')='string' AND jsonb_typeof(metadata->'oid')='string'
    )
);
CREATE UNIQUE INDEX ci_checkout_single_source ON radishnexus.entity_links(workspace_id,from_id)
WHERE from_type='ci-run' AND relation_type='built-from' AND to_type='repository';
CREATE INDEX repository_ci_runs ON radishnexus.entity_links(workspace_id,to_id,from_id)
WHERE from_type='ci-run' AND relation_type='built-from' AND to_type='repository' AND state='active';

-- Evaluate complete facts at commit: event/receipt/relationship may be inserted
-- in any order inside the one transaction, but no partial checkout can escape.
CREATE FUNCTION radishnexus.assert_ci_checkout(ws text, run_id text) RETURNS void
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE
    c ci_runs%ROWTYPE;
    b ci_source_checkout_bindings%ROWTYPE;
    link_count integer;
    n bigint;
BEGIN
    SELECT * INTO c FROM ci_runs WHERE workspace_id=ws AND id=run_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'CI checkout endpoint missing' USING ERRCODE='23514';
    END IF;
    SELECT * INTO b FROM ci_source_checkout_bindings WHERE workspace_id=ws AND source_kind=c.source_kind AND source_id=c.source_id;
    IF FOUND THEN
        IF c.component_id<>b.component_id OR c.external_run_key !~ '^[1-9][0-9]{0,9}$' THEN
            RAISE EXCEPTION 'CI checkout source identity mismatch' USING ERRCODE='23514';
        END IF;
        n := c.external_run_key::bigint;
        IF n>2147483647 OR (n>=b.effective_from_build)<>(c.checkout_state<>'not_collected') THEN
            RAISE EXCEPTION 'CI checkout version boundary mismatch' USING ERRCODE='23514';
        END IF;
    END IF;
    SELECT count(*) INTO link_count FROM entity_links WHERE workspace_id=ws AND from_type='ci-run' AND from_id=run_id AND relation_type='built-from';
    IF link_count<>(CASE WHEN c.checkout_state='observed' THEN 1 ELSE 0 END) THEN
        RAISE EXCEPTION 'CI checkout state and relationship disagree' USING ERRCODE='23514';
    END IF;
    IF c.checkout_state='not_collected' THEN RETURN; END IF;
    IF NOT EXISTS (
        SELECT 1 FROM inbound_deliveries d
        JOIN domain_events e ON e.workspace_id=d.workspace_id AND e.event_id=d.event_id
        LEFT JOIN entity_links l ON l.workspace_id=ws AND l.from_type='ci-run' AND l.from_id=run_id AND l.relation_type='built-from'
        WHERE d.workspace_id=ws AND d.ci_run_id=run_id AND d.source_kind=c.source_kind AND d.source_id=c.source_id
        AND d.delivery_id='build-'||c.external_run_key AND d.recorded_at=c.created_at
        AND e.event_type='ci-run.recorded' AND e.schema_version=2
        AND e.primary_entity_type='ci-run' AND e.primary_entity_id=run_id
        AND e.actor_kind='plugin' AND e.actor_id=c.source_id AND e.source_kind='plugin' AND e.source_id=c.source_id
        AND e.project_id IS NULL AND e.occurred_at=c.completed_at
        AND e.payload=jsonb_build_object('status',c.status,'component',jsonb_build_object('type','component','id',c.component_id),
            'checkout_state',c.checkout_state,'checkout_reason',c.checkout_reason,
            'repository',jsonb_build_object('type','repository','id',b.repository_id),'link_id',l.id)
        AND (c.checkout_state='unavailable' OR (
            l.to_type='repository' AND l.to_id=b.repository_id AND l.created_by_id=c.source_id
            AND l.origin_ref=d.delivery_id AND l.source_event_id=e.event_id AND l.created_at=c.created_at AND l.updated_at=c.created_at
            AND l.metadata->>'binding_id'=b.binding_id AND l.metadata->>'profile'=b.profile))
    ) THEN
        RAISE EXCEPTION 'CI checkout provenance must match its receipt and event' USING ERRCODE='23514';
    END IF;
END;
$$;

CREATE FUNCTION radishnexus.check_ci_checkout_consistency() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE run_id text;
BEGIN
    IF TG_TABLE_NAME='ci_runs' THEN run_id := NEW.id;
    ELSIF TG_TABLE_NAME='entity_links' THEN run_id := NEW.from_id;
    ELSIF TG_TABLE_NAME='domain_events' THEN run_id := NEW.primary_entity_id;
    ELSE run_id := NEW.ci_run_id;
    END IF;
    PERFORM assert_ci_checkout(NEW.workspace_id,run_id);
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER ci_checkout_consistency AFTER INSERT OR UPDATE ON radishnexus.ci_runs
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.check_ci_checkout_consistency();
CREATE CONSTRAINT TRIGGER ci_checkout_link_consistency AFTER INSERT OR UPDATE ON radishnexus.entity_links
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.from_type='ci-run' AND NEW.relation_type='built-from')
EXECUTE FUNCTION radishnexus.check_ci_checkout_consistency();
CREATE CONSTRAINT TRIGGER ci_checkout_event_consistency AFTER INSERT ON radishnexus.domain_events
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.event_type='ci-run.recorded' AND NEW.schema_version=2)
EXECUTE FUNCTION radishnexus.check_ci_checkout_consistency();
CREATE CONSTRAINT TRIGGER ci_checkout_receipt_consistency AFTER INSERT ON radishnexus.inbound_deliveries
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.check_ci_checkout_consistency();

CREATE FUNCTION radishnexus.check_ci_checkout_binding_history() RETURNS trigger
LANGUAGE plpgsql SET search_path=radishnexus,pg_temp AS $$
DECLARE c record;
BEGIN
    FOR c IN SELECT id FROM ci_runs WHERE workspace_id=NEW.workspace_id AND source_kind=NEW.source_kind AND source_id=NEW.source_id LOOP
        PERFORM assert_ci_checkout(NEW.workspace_id,c.id);
    END LOOP;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER ci_checkout_binding_history AFTER INSERT ON radishnexus.ci_source_checkout_bindings
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION radishnexus.check_ci_checkout_binding_history();

UPDATE radishnexus.activity_items SET projection_version=6 WHERE projection_version=5;

---- create above / drop below ----
-- Forward-only. Restore the pre-upgrade backup to a new target for version rollback.
