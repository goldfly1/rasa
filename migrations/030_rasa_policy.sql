-- Database: rasa_policy
-- Policy Engine — rules, audit log, human review queue

\c rasa_policy;

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- 4-layer rule evaluation --------------------------------------------------
CREATE TYPE rule_scope AS ENUM ('org', 'soul', 'task', 'human');
CREATE TYPE rule_action AS ENUM ('allow', 'deny', 'escalate', 'rate_limit');

CREATE TABLE policy_rules (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    scope       rule_scope NOT NULL,
    priority    INTEGER NOT NULL DEFAULT 100,
    match_field TEXT NOT NULL,
    match_op    TEXT NOT NULL,
    match_value TEXT NOT NULL,
    action      rule_action NOT NULL DEFAULT 'deny',
    action_params JSONB NOT NULL DEFAULT '{}',
    description TEXT,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_rules_scope ON policy_rules(scope);
CREATE INDEX idx_rules_priority ON policy_rules(priority DESC);

-- Audit log ---------------------------------------------------------------
CREATE TABLE audit_log (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id     UUID,
    agent_id    TEXT,
    rule_id     UUID,
    decision    rule_action NOT NULL,
    context     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_task ON audit_log(task_id);
CREATE INDEX idx_audit_agent ON audit_log(agent_id);
CREATE INDEX idx_audit_time ON audit_log(created_at DESC);

-- Human review queue -------------------------------------------------------
CREATE TABLE human_reviews (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id     UUID NOT NULL,
    agent_id    TEXT NOT NULL,
    reason      TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'pending',
    reviewer    TEXT,
    resolved_at TIMESTAMP WITH TIME ZONE,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_hr_status ON human_reviews(status);
CREATE INDEX idx_hr_task ON human_reviews(task_id);
