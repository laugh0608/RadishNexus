-- Preserve stable users and business references; account state is independent
-- of whether the user has a local password.
CREATE TABLE radishnexus.user_accounts (
    user_id text PRIMARY KEY REFERENCES radishnexus.users(id),
    status text NOT NULL CHECK (status IN ('active', 'disabled')),
    created_at timestamptz NOT NULL
);
INSERT INTO radishnexus.user_accounts (user_id, status, created_at)
SELECT user_id, status, created_at FROM radishnexus.local_accounts;

DROP TRIGGER local_accounts_control_identity ON radishnexus.local_accounts;
DROP FUNCTION radishnexus.enforce_local_account_identity();
ALTER TABLE radishnexus.local_accounts RENAME TO local_credentials;
ALTER TABLE radishnexus.local_credentials RENAME COLUMN login_name TO legacy_login_name;
ALTER TABLE radishnexus.local_credentials ALTER COLUMN legacy_login_name DROP NOT NULL;
ALTER TABLE radishnexus.local_credentials ADD COLUMN email text UNIQUE;
ALTER TABLE radishnexus.local_credentials ADD CONSTRAINT local_credentials_account
    FOREIGN KEY (user_id) REFERENCES radishnexus.user_accounts(user_id);
ALTER TABLE radishnexus.local_credentials ADD CONSTRAINT local_credentials_email CHECK (
    email IS NULL OR (
        octet_length(email) BETWEEN 3 AND 254 AND email = lower(email)
        AND email ~ '^[a-z0-9!#$%&''*+/=?^_`{|}~.-]+@[a-z0-9.-]+$'
        AND position('..' in email) = 0
        AND length(split_part(email, '@', 1)) BETWEEN 1 AND 64
        AND split_part(email, '@', 1) NOT LIKE '.%'
        AND split_part(email, '@', 1) NOT LIKE '%.'
        AND split_part(email, '@', 2) ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$'
    )
);
ALTER TABLE radishnexus.local_credentials ADD CONSTRAINT local_credentials_identifier
    CHECK (email IS NOT NULL OR legacy_login_name IS NOT NULL);

CREATE FUNCTION radishnexus.enforce_local_credential_identity()
RETURNS trigger LANGUAGE plpgsql SET search_path = radishnexus, pg_temp AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'local credentials cannot be deleted' USING ERRCODE = '23514';
    END IF;
    IF NEW.user_id <> OLD.user_id OR NEW.created_at <> OLD.created_at
       OR NEW.legacy_login_name IS DISTINCT FROM OLD.legacy_login_name
       OR (OLD.email IS NOT NULL AND NEW.email IS DISTINCT FROM OLD.email) THEN
        RAISE EXCEPTION 'local credential identity cannot change' USING ERRCODE = '23514';
    END IF;
    IF NEW.password_hash <> OLD.password_hash AND NEW.password_changed_at <= OLD.password_changed_at THEN
        RAISE EXCEPTION 'password changes require a later timestamp' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER local_credentials_control_identity
BEFORE UPDATE OR DELETE ON radishnexus.local_credentials
FOR EACH ROW EXECUTE FUNCTION radishnexus.enforce_local_credential_identity();

-- Upgrade requires fresh authentication on the new account boundary.
UPDATE radishnexus.user_sessions SET revoked_at = greatest(clock_timestamp(), created_at)
WHERE revoked_at IS NULL;

CREATE TABLE radishnexus.external_identities (
    user_id text NOT NULL REFERENCES radishnexus.user_accounts(user_id),
    issuer text NOT NULL CHECK (length(issuer) BETWEEN 1 AND 2048),
    subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 255),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (issuer, subject),
    UNIQUE (user_id, issuer)
);

CREATE TABLE radishnexus.identity_invitations (
    id text PRIMARY KEY CHECK (id LIKE 'inv_%'),
    token_digest bytea NOT NULL UNIQUE CHECK (octet_length(token_digest) = 32),
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces(id),
    created_by text NOT NULL REFERENCES radishnexus.user_accounts(user_id),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at),
    consumed_at timestamptz,
    CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);
CREATE INDEX identity_invitations_expiry ON radishnexus.identity_invitations(expires_at);

CREATE TABLE radishnexus.oidc_transactions (
    state_digest bytea PRIMARY KEY CHECK (octet_length(state_digest) = 32),
    browser_digest bytea NOT NULL CHECK (octet_length(browser_digest) = 32),
    nonce_digest bytea NOT NULL CHECK (octet_length(nonce_digest) = 32),
    provider_digest bytea NOT NULL CHECK (octet_length(provider_digest) = 32),
    initiating_session_digest bytea CHECK (initiating_session_digest IS NULL OR octet_length(initiating_session_digest) = 32),
    invitation_digest bytea CHECK (invitation_digest IS NULL OR octet_length(invitation_digest) = 32),
    display_name text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at),
    CHECK (initiating_session_digest IS NULL OR invitation_digest IS NULL)
);
CREATE INDEX oidc_transactions_expiry ON radishnexus.oidc_transactions(expires_at);

CREATE TABLE radishnexus.identity_audit (
    id text PRIMARY KEY CHECK (id LIKE 'ida_%'),
    action text NOT NULL CHECK (action IN ('invitation.created', 'invitation.accepted', 'external.linked', 'external.unlinked', 'external.login', 'local.account.created')),
    actor_id text NOT NULL REFERENCES radishnexus.users(id),
    workspace_id text REFERENCES radishnexus.workspaces(id),
    occurred_at timestamptz NOT NULL
);

---- create above / drop below ----
-- Forward-only. Restore the pre-upgrade backup into an empty target to roll back.
