ALTER TABLE radishnexus.workspace_memberships
ADD COLUMN role text NOT NULL DEFAULT 'member';

ALTER TABLE radishnexus.workspace_memberships
ADD CONSTRAINT workspace_memberships_role
CHECK (role IN ('owner', 'member'));

CREATE TABLE radishnexus.local_accounts (
    user_id text PRIMARY KEY REFERENCES radishnexus.users (id),
    login_name text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    status text NOT NULL DEFAULT 'active',
    failed_login_count integer NOT NULL DEFAULT 0,
    locked_until timestamptz,
    created_at timestamptz NOT NULL,
    password_changed_at timestamptz NOT NULL,
    last_authenticated_at timestamptz,
    CONSTRAINT local_accounts_login_name CHECK (
        login_name ~ '^[a-z0-9][a-z0-9._-]{2,63}$'
    ),
    CONSTRAINT local_accounts_password_hash_nonempty CHECK (
        btrim(password_hash) <> ''
    ),
    CONSTRAINT local_accounts_status CHECK (status IN ('active', 'disabled')),
    CONSTRAINT local_accounts_failed_login_count CHECK (failed_login_count >= 0),
    CONSTRAINT local_accounts_password_time CHECK (password_changed_at >= created_at),
    CONSTRAINT local_accounts_last_authenticated_time CHECK (
        last_authenticated_at IS NULL OR last_authenticated_at >= created_at
    )
);

CREATE TABLE radishnexus.user_sessions (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES radishnexus.users (id),
    token_digest bytea NOT NULL UNIQUE,
    csrf_token_digest bytea NOT NULL,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    CONSTRAINT user_sessions_id_format CHECK (id LIKE 'ses_%'),
    CONSTRAINT user_sessions_token_digest_length CHECK (octet_length(token_digest) = 32),
    CONSTRAINT user_sessions_csrf_token_digest_length CHECK (
        octet_length(csrf_token_digest) = 32
    ),
    CONSTRAINT user_sessions_expiry CHECK (expires_at > created_at),
    CONSTRAINT user_sessions_revocation_time CHECK (
        revoked_at IS NULL OR revoked_at >= created_at
    )
);

CREATE FUNCTION radishnexus.enforce_local_account_identity()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'local accounts cannot be deleted'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.user_id <> OLD.user_id
       OR NEW.login_name <> OLD.login_name
       OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'local account identity cannot change'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.password_hash <> OLD.password_hash
       AND NEW.password_changed_at <= OLD.password_changed_at THEN
        RAISE EXCEPTION 'password changes require a later password_changed_at'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER local_accounts_control_identity
BEFORE UPDATE OR DELETE ON radishnexus.local_accounts
FOR EACH ROW EXECUTE FUNCTION radishnexus.enforce_local_account_identity();

CREATE FUNCTION radishnexus.enforce_user_session_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = radishnexus, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.revoked_at IS NULL AND OLD.expires_at > clock_timestamp() THEN
            RAISE EXCEPTION 'active user sessions cannot be deleted'
                USING ERRCODE = '23514';
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.id <> OLD.id
       OR NEW.user_id <> OLD.user_id
       OR NEW.token_digest <> OLD.token_digest
       OR NEW.csrf_token_digest <> OLD.csrf_token_digest
       OR NEW.created_at <> OLD.created_at
       OR NEW.expires_at <> OLD.expires_at THEN
        RAISE EXCEPTION 'user session identity and expiry cannot change'
            USING ERRCODE = '23514';
    END IF;
    IF OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at THEN
        RAISE EXCEPTION 'revoked user sessions cannot transition again'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER user_sessions_control_mutation
BEFORE UPDATE OR DELETE ON radishnexus.user_sessions
FOR EACH ROW EXECUTE FUNCTION radishnexus.enforce_user_session_mutation();

CREATE INDEX user_sessions_active_user
ON radishnexus.user_sessions (user_id, expires_at DESC)
WHERE revoked_at IS NULL;

CREATE INDEX user_sessions_expiry
ON radishnexus.user_sessions (expires_at)
WHERE revoked_at IS NULL;

---- create above / drop below ----

DROP INDEX radishnexus.user_sessions_expiry;
DROP INDEX radishnexus.user_sessions_active_user;
DROP TRIGGER user_sessions_control_mutation ON radishnexus.user_sessions;
DROP FUNCTION radishnexus.enforce_user_session_mutation();
DROP TRIGGER local_accounts_control_identity ON radishnexus.local_accounts;
DROP FUNCTION radishnexus.enforce_local_account_identity();
DROP TABLE radishnexus.user_sessions;
DROP TABLE radishnexus.local_accounts;
ALTER TABLE radishnexus.workspace_memberships
DROP CONSTRAINT workspace_memberships_role;
ALTER TABLE radishnexus.workspace_memberships DROP COLUMN role;
