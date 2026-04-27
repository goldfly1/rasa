-- Database: rasa_orch
-- Orchestrator — task lifecycle, DAG nodes, checkpoints, submissions

\c rasa_orch;

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Task lifecycle state machine ---------------------------------------------
CREATE TYPE task_status AS ENUM (
    'PENDING', 'ASSIGNED', 'RUNNING', 'PAUSED',
    'CHECKPOINTED', 'COMPLETED', 'FAILED', 'CANCELLED'
);

CREATE TABLE tasks (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    correlation_id UUID NOT NULL DEFAULT uuid_generate_v4(),
    parent_id     UUID REFERENCES tasks(id) ON DELETE SET NULL,
    title         TEXT NOT NULL,
    description   TEXT,
    payload       JSONB NOT NULL DEFAULT '{}',
    status        task_status NOT NULL DEFAULT 'PENDING',
    soul_id       TEXT NOT NULL,
    assigned_agent_id TEXT,
    priority      INTEGER NOT NULL DEFAULT 5,
    retry_count   INTEGER NOT NULL DEFAULT 0,
    max_retries   INTEGER NOT NULL DEFAULT 3,
    retry_after   TIMESTAMP WITH TIME ZONE,
    created_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    assigned_at   TIMESTAMP WITH TIME ZONE,
    started_at    TIMESTAMP WITH TIME ZONE,
    completed_at  TIMESTAMP WITH TIME ZONE,
    failed_at     TIMESTAMP WITH TIME ZONE,
    result        JSONB,
    error_message TEXT
);

CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_soul ON tasks(soul_id);
CREATE INDEX idx_tasks_agent ON tasks(assigned_agent_id);
CREATE INDEX idx_tasks_created ON tasks(created_at DESC);
CREATE INDEX idx_tasks_parent ON tasks(parent_id);

-- Task dependencies / DAG edges ---------------------------------------------
CREATE TABLE task_dependencies (
    from_task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    to_task_id   UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    dep_type     TEXT NOT NULL DEFAULT 'finish-to-start',
    PRIMARY KEY (from_task_id, to_task_id)
);

CREATE INDEX idx_task_deps_from ON task_dependencies(from_task_id);
CREATE INDEX idx_task_deps_to   ON task_dependencies(to_task_id);

-- Checkpoint refs ----------------------------------------------------------
CREATE TABLE checkpoint_refs (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id      UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    agent_id     TEXT NOT NULL,
    snapshot_path TEXT NOT NULL,
    metadata     JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ckpt_task ON checkpoint_refs(task_id);
CREATE INDEX idx_ckpt_agent ON checkpoint_refs(agent_id);
