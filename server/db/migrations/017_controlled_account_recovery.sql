-- Preserve existing user audit semantics; recovery is a global operator action.
ALTER TABLE radishnexus.identity_audit DROP CONSTRAINT identity_audit_action_check;
ALTER TABLE radishnexus.identity_audit ALTER COLUMN actor_id DROP NOT NULL;
ALTER TABLE radishnexus.identity_audit
    ADD COLUMN actor_kind text NOT NULL DEFAULT 'user',
    ADD COLUMN operator_ref text,
    ADD COLUMN target_user_id text REFERENCES radishnexus.user_accounts(user_id),
    ADD COLUMN operation_id uuid UNIQUE,
    ADD COLUMN reason_code text,
    ADD COLUMN previous_password_changed_at timestamptz,
    ADD COLUMN applied_password_changed_at timestamptz,
    ADD CONSTRAINT identity_audit_shape CHECK (
        (actor_kind = 'user' AND actor_id IS NOT NULL
         AND action IN ('invitation.created', 'invitation.accepted', 'external.linked', 'external.unlinked', 'external.login', 'local.account.created')
         AND operator_ref IS NULL AND target_user_id IS NULL AND operation_id IS NULL
         AND reason_code IS NULL AND previous_password_changed_at IS NULL AND applied_password_changed_at IS NULL)
        OR
        (actor_kind = 'operator' AND actor_id IS NULL AND workspace_id IS NULL
         AND action = 'local.password.recovered'
         AND operator_ref IS NOT NULL AND operator_ref ~ '^[A-Za-z0-9_.-]{1,64}$'
         AND target_user_id IS NOT NULL AND operation_id IS NOT NULL
         AND operation_id::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
         AND reason_code IS NOT NULL AND reason_code = 'lost_local_password'
         AND previous_password_changed_at IS NOT NULL AND applied_password_changed_at IS NOT NULL
         AND applied_password_changed_at > previous_password_changed_at
         AND occurred_at = applied_password_changed_at)
    );

CREATE FUNCTION radishnexus.enforce_recovery_audit_immutable()
RETURNS trigger LANGUAGE plpgsql SET search_path = radishnexus, pg_temp AS $$
BEGIN
    IF OLD.action = 'local.password.recovered' THEN
        RAISE EXCEPTION 'recovery audit is immutable' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.action = 'local.password.recovered' THEN
        RAISE EXCEPTION 'existing audit cannot become recovery' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER identity_audit_recovery_immutable
BEFORE UPDATE OR DELETE ON radishnexus.identity_audit
FOR EACH ROW EXECUTE FUNCTION radishnexus.enforce_recovery_audit_immutable();

---- create above / drop below ----
-- Forward-only. Restore a verified backup into a new isolated empty target.
