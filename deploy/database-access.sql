-- Run as the schema owner after migration or empty-target restore.
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL search_path = pg_catalog, pg_temp;

REVOKE ALL ON public.radishnexus_schema_migrations FROM PUBLIC, radishnexus_app, radishnexus_backup;
GRANT SELECT ON public.radishnexus_schema_migrations TO radishnexus_app, radishnexus_backup;
REVOKE ALL ON SCHEMA radishnexus FROM PUBLIC, radishnexus_app, radishnexus_backup;
GRANT USAGE ON SCHEMA radishnexus TO radishnexus_app, radishnexus_backup;
REVOKE EXECUTE ON ALL ROUTINES IN SCHEMA radishnexus FROM PUBLIC;
GRANT EXECUTE ON ALL ROUTINES IN SCHEMA radishnexus TO radishnexus_app;
REVOKE ALL ON ALL TABLES IN SCHEMA radishnexus FROM PUBLIC, radishnexus_app, radishnexus_backup;
GRANT SELECT ON ALL TABLES IN SCHEMA radishnexus TO radishnexus_app, radishnexus_backup;
GRANT INSERT, UPDATE ON ALL TABLES IN SCHEMA radishnexus TO radishnexus_app;
GRANT DELETE ON radishnexus.activity_items, radishnexus.channel_memberships, radishnexus.external_identities,
    radishnexus.identity_invitations, radishnexus.oidc_transactions, radishnexus.project_memberships,
    radishnexus.thread_memberships TO radishnexus_app;
REVOKE INSERT, UPDATE ON radishnexus.entity_types, radishnexus.relation_types FROM radishnexus_app;
REVOKE UPDATE ON radishnexus.identity_audit, radishnexus.workspace_configuration_audit,
    radishnexus.context_export_audit FROM radishnexus_app;
-- Project import locks the tables it requires empty, including export audit.
-- MAINTAIN allows that lock without granting UPDATE, DELETE or TRUNCATE.
GRANT MAINTAIN ON radishnexus.context_export_audit TO radishnexus_app;
COMMIT;
