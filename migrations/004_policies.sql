-- Policy engine for enforcing configurable rules beyond RBAC
CREATE TABLE policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    policy_type TEXT NOT NULL CHECK (policy_type IN ('time_based', 'content', 'rate_of_change')),
    enabled BOOLEAN NOT NULL DEFAULT true,
    config JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_policies_enabled ON policies (enabled) WHERE enabled = true;
CREATE INDEX idx_policies_type ON policies (policy_type);
