-- Run only on the stopped, dedicated PostgreSQL Compose instance.
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL search_path = pg_catalog, pg_temp;
SET LOCAL password_encryption = 'scram-sha-256';
SET LOCAL log_statement = 'none';
SET LOCAL log_min_error_statement = 'panic';

DO $$
DECLARE
    role_name text;
    role_oid oid;
    object record;
BEGIN
    IF current_user <> 'radishnexus' OR current_database() <> 'radishnexus' OR
       current_setting('server_version_num')::integer / 10000 <> 17 THEN
        RAISE EXCEPTION 'role provisioning requires the PostgreSQL 17 Compose administrator';
    END IF;
    IF EXISTS (SELECT FROM pg_stat_activity WHERE datname = current_database()
               AND backend_type = 'client backend' AND pid <> pg_backend_pid()) THEN
        RAISE EXCEPTION 'stop all other database clients before provisioning roles';
    END IF;

    FOREACH role_name IN ARRAY ARRAY['radishnexus_owner', 'radishnexus_app', 'radishnexus_backup']
    LOOP
        SELECT oid INTO role_oid FROM pg_roles WHERE rolname = role_name;
        IF NOT FOUND THEN
            EXECUTE format('CREATE ROLE %I LOGIN NOINHERIT', role_name);
        ELSIF EXISTS (SELECT FROM pg_roles WHERE oid = role_oid
                      AND (rolsuper OR rolcreaterole OR rolcreatedb OR rolreplication OR rolbypassrls))
           OR EXISTS (SELECT FROM pg_auth_members WHERE member = role_oid OR roleid = role_oid)
           OR (role_name <> 'radishnexus_owner' AND EXISTS (
               SELECT FROM pg_shdepend WHERE refclassid = 'pg_authid'::regclass
                 AND refobjid = role_oid AND deptype = 'o'
           )) THEN
            RAISE EXCEPTION 'unexpected ownership, privileges or membership for role %', role_name;
        ELSE
            EXECUTE format('ALTER ROLE %I LOGIN NOINHERIT', role_name);
        END IF;
    END LOOP;

    -- Transfer only the supported project objects. The bootstrap administrator
    -- also owns cluster/system objects and must never use REASSIGN OWNED here.
    FOR object IN
        SELECT 'SCHEMA' AS kind, quote_ident(nspname) AS name, nspowner AS owner, 1 AS priority
        FROM pg_namespace WHERE nspname = 'radishnexus'
        UNION ALL
        SELECT 'TABLE', format('%I.%I', nspname, relname), relowner, 2
        FROM pg_class JOIN pg_namespace ON relnamespace = pg_namespace.oid
        WHERE relkind = 'r' AND (nspname = 'radishnexus' OR
              (nspname = 'public' AND relname = 'radishnexus_schema_migrations'))
        UNION ALL
        SELECT 'FUNCTION', pg_proc.oid::regprocedure::text, proowner, 3
        FROM pg_proc JOIN pg_namespace ON pronamespace = pg_namespace.oid
        WHERE nspname = 'radishnexus' AND prokind = 'f'
        ORDER BY priority, name
    LOOP
        IF object.owner = 'radishnexus'::regrole THEN
            EXECUTE format('ALTER %s %s OWNER TO radishnexus_owner', object.kind, object.name);
        ELSIF object.owner <> 'radishnexus_owner'::regrole THEN
            RAISE EXCEPTION 'unexpected owner for % %', object.kind, object.name;
        END IF;
    END LOOP;
END
$$;

REVOKE ALL ON DATABASE radishnexus FROM PUBLIC;
REVOKE CONNECT, TEMPORARY ON DATABASE postgres, template1 FROM PUBLIC;
GRANT CONNECT ON DATABASE radishnexus TO radishnexus_owner, radishnexus_app, radishnexus_backup;
GRANT CREATE ON DATABASE radishnexus TO radishnexus_owner;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT CREATE ON SCHEMA public TO radishnexus_owner;
GRANT EXECUTE ON FUNCTION pg_catalog.pg_control_system() TO radishnexus_app;
-- Large objects are outside the authoritative backup inventory. Their
-- creation functions otherwise let even roles without schema CREATE add data.
REVOKE EXECUTE ON FUNCTION pg_catalog.lo_create(oid), pg_catalog.lo_creat(integer),
    pg_catalog.lo_from_bytea(oid, bytea) FROM PUBLIC;

\password radishnexus_owner
\password radishnexus_app
\password radishnexus_backup
COMMIT;
