CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- API keys for authenticating clients
CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL UNIQUE,
    key_prefix TEXT NOT NULL,
    expires_at TIMESTAMPTZ,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_api_keys_key_hash ON api_keys (key_hash);
CREATE INDEX idx_api_keys_active ON api_keys (active) WHERE active = true;

-- Roles for grouping permissions
CREATE TABLE roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Permissions define what actions a role can perform on which resources
CREATE TABLE permissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    resource TEXT NOT NULL,
    action TEXT NOT NULL,
    UNIQUE (role_id, resource, action)
);

CREATE INDEX idx_permissions_role_id ON permissions (role_id);

-- Join table between API keys and roles (many-to-many)
CREATE TABLE api_key_roles (
    api_key_id UUID NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (api_key_id, role_id)
);

-- Per-key rate limits
CREATE TABLE rate_limits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    api_key_id UUID NOT NULL UNIQUE REFERENCES api_keys(id) ON DELETE CASCADE,
    requests_per_min INT NOT NULL DEFAULT 60,
    requests_per_hour INT NOT NULL DEFAULT 1000,
    requests_per_day INT NOT NULL DEFAULT 10000
);

-- Audit log for all proxied requests
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    api_key_id UUID NOT NULL REFERENCES api_keys(id),
    action TEXT NOT NULL,
    resource TEXT NOT NULL,
    status_code INT NOT NULL,
    latency_ms BIGINT NOT NULL,
    ip TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_logs_api_key_id ON audit_logs (api_key_id);
CREATE INDEX idx_audit_logs_created_at ON audit_logs (created_at);
