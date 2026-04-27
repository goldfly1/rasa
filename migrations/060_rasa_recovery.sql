-- Database: rasa_recovery
-- Recovery Controller — idempotency ledger, recovery log

\c rasa_recovery;

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Idempotency ledger -------------------------------------------------------
CREATE TABLE idempotency_ledger (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    key_hash    TEXT NOT NULL UNIQUE,
    key_plain   TEXT NOT NULL,
    operation   TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    result      JSONB,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_il_hash ON idempotency_ledger(key_hash);
CREATE INDEX idx_il_status ON idempotency_ledger(status);
CREATE INDEX idx_il_time ON idempotency_ledger(created_at DESC);

-- Recovery log ------------------------------------------------------------
CREATE TABLE recovery_log (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id     UUID NOT NULL,
    agent_id    TEXT NOT NULL,
    checkpoint_id UUID,
    soul_version TEXT,
    soul_mismatch TEXT,
    action      TEXT NOT NULL,
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_rl_task ON recovery_log(task_id);
CREATE INDEX idx_rl_agent ON recovery_log(agent_id);
CREATE INDEX idx_rl_time ON recovery_log(created_at DESC);
