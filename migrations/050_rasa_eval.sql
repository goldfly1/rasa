-- Database: rasa_eval
-- Evaluation Engine — records, benchmarks, drift

\c rasa_eval;

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Evaluation records ------------------------------------------------------
CREATE TABLE evaluation_records (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id     UUID NOT NULL,
    agent_id    TEXT NOT NULL,
    soul_id     TEXT NOT NULL,
    benchmark   TEXT NOT NULL,
    score       NUMERIC(5,4) NOT NULL,
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_eval_task ON evaluation_records(task_id);
CREATE INDEX idx_eval_agent ON evaluation_records(agent_id);
CREATE INDEX idx_eval_soul ON evaluation_records(soul_id);
CREATE INDEX idx_eval_time ON evaluation_records(created_at DESC);

-- Drift window (20-task rolling aggregate) --------------------------------
CREATE TABLE drift_snapshots (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    agent_id    TEXT NOT NULL,
    soul_id     TEXT NOT NULL,
    window_size INTEGER NOT NULL DEFAULT 20,
    mean_score  NUMERIC(5,4),
    std_score   NUMERIC(5,4),
    flagged     BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_drift_agent ON drift_snapshots(agent_id);
CREATE INDEX idx_drift_time ON drift_snapshots(created_at DESC);
