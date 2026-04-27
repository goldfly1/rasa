-- Database: rasa_pool
-- Pool Controller — agent registry, heartbeat ledger, backpressure

\c rasa_pool;

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Agent registry (durable) -------------------------------------------------
CREATE TYPE agent_state AS ENUM (
    'REGISTERED', 'WARMING', 'ACTIVE', 'PAUSED',
    'CHECKPOINTED', 'DRAINING', 'UNRESPONSIVE', 'DISCONNECTED'
);

CREATE TABLE agents (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    agent_id        TEXT NOT NULL UNIQUE,
    soul_id         TEXT NOT NULL,
    hostname        TEXT NOT NULL,
    state           agent_state NOT NULL DEFAULT 'REGISTERED',
    capabilities    JSONB NOT NULL DEFAULT '[]',
    labels          JSONB NOT NULL DEFAULT '{}',
    last_heartbeat  TIMESTAMP WITH TIME ZONE,
    registered_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    disconnected_at TIMESTAMP WITH TIME ZONE,
    checkpoint_ref  UUID
);

CREATE INDEX idx_agents_state ON agents(state);
CREATE INDEX idx_agents_soul  ON agents(soul_id);
CREATE INDEX idx_agents_heartbeat ON agents(last_heartbeat);

-- Heatbeat ledger (oldest first eviction) ----------------------------------
CREATE TABLE heartbeats (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    agent_id    TEXT NOT NULL,
    seq_num     BIGINT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    received_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_hb_agent ON heartbeats(agent_id);
CREATE INDEX idx_hb_received ON heartbeats(received_at);

-- Backpressure log ---------------------------------------------------------
CREATE TABLE backpressure_events (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    reason      TEXT NOT NULL,
    agents_busy INTEGER NOT NULL DEFAULT 0,
    agents_idle INTEGER NOT NULL DEFAULT 0,
    queue_depth INTEGER NOT NULL DEFAULT 0,
    triggered_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    released_at  TIMESTAMP WITH TIME ZONE
);
