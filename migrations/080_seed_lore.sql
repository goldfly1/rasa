-- Migration 080: Seed project lore into rasa_memory
-- Populates canonical_nodes with the full system architecture graph.
-- Run: psql -U postgres -f migrations/080_seed_lore.sql
--
-- Idempotent: uses ON CONFLICT (name, node_type) DO NOTHING so re-runs are safe.

\c rasa_memory;

-- Add a uniqueness constraint so re-runs don't duplicate --------------------
DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'uq_cn_name_type'
    ) THEN
        ALTER TABLE canonical_nodes ADD CONSTRAINT uq_cn_name_type UNIQUE (name, node_type);
    END IF;
END $$;

-- ============================================================================
-- 1. System / Infrastructure nodes
-- ============================================================================
INSERT INTO canonical_nodes (node_type, name, path, body)
VALUES
('system', 'rasa-platform',        '/',                    '{"description":"RASA — Reliable Autonomous System of Agents. Multi-agent orchestration platform for single-node lab machine.","phase":"pilot","gates_complete":5,"hardware":"Intel Ultra 7 255, 64GB RAM, RTX 5060 8GB VRAM, 1TB SSD"}'::jsonb),
('infra', 'postgresql',            '/infra/postgresql',    '{"description":"PostgreSQL 16+ — sole durable message bus. 6 databases: rasa_orch, rasa_pool, rasa_policy, rasa_memory, rasa_eval, rasa_recovery. LISTEN/NOTIFY for wake-up, backing tables for durability.","version":"16+","role":"durable-bus"}'::jsonb),
('infra', 'redis',                 '/infra/redis',         '{"description":"Redis 7.x — ephemeral transport only. Pub/Sub for heartbeats and policy updates. Session store for conversation turns. SHA-256 prompt cache for LLM Gateway. Single-node for pilot.","version":"7.x","role":"ephemeral-transport"}'::jsonb),
('infra', 'ollama-cloud',          '/infra/ollama',        '{"description":"Ollama Cloud — LLM provider. Standard tier: Deepseek-v4-flash:cloud. Premium tier: Deepseek-v4-pro:cloud. Model selection via RASA_DEFAULT_MODEL / RASA_PREMIUM_MODEL env vars.","models":["Deepseek-v4-flash:cloud","Deepseek-v4-pro:cloud"]}'::jsonb),
('infra', 'honcho',                '/infra/honcho',        '{"description":"honcho — Python Procfile runner. Single command starts all services. Ctrl-C shuts down constellation. No Docker/K8s in pilot."}'::jsonb)
ON CONFLICT (name, node_type) DO NOTHING;

-- ============================================================================
-- 2. Database nodes (6 databases)
-- ============================================================================
INSERT INTO canonical_nodes (node_type, name, path, body)
VALUES
('database', 'rasa_orch',     '/databases/orch',     '{"description":"Orchestrator database. Tables: tasks (state machine), bus_messages (delivery audit), checkpoint_refs. Views: v_task_latency, v_daily_summary.","owner":"orchestrator"}'::jsonb),
('database', 'rasa_pool',     '/databases/pool',     '{"description":"Pool database. Tables: agents (registration), heartbeats (durable heartbeat ledger), backpressure_events (saturation log). Views: v_agent_uptime, v_recent_backpressure.","owner":"pool-controller"}'::jsonb),
('database', 'rasa_policy',   '/databases/policy',   '{"description":"Policy database. Tables: rules, audit_log, human_reviews. View: v_recent_decisions.","owner":"policy-engine"}'::jsonb),
('database', 'rasa_memory',   '/databases/memory',   '{"description":"Memory database. Tables: canonical_nodes (this graph), embeddings (pgvector HNSW), soul_sheets (runtime cache).","owner":"memory-controller"}'::jsonb),
('database', 'rasa_eval',     '/databases/eval',     '{"description":"Evaluation database. Tables: evaluation_records (per-task scores), drift_snapshots (window materialization). Views: v_soul_performance, v_latest_drift.","owner":"eval-aggregator"}'::jsonb),
('database', 'rasa_recovery', '/databases/recovery', '{"description":"Recovery database. Tables: recovery_log (action audit), idempotency_ledger (ON CONFLICT UPSERT by key_hash). View: v_recent_recoveries.","owner":"recovery-controller"}'::jsonb)
ON CONFLICT (name, node_type) DO NOTHING;

-- ============================================================================
-- 3. Control plane components (Go)
-- ============================================================================
INSERT INTO canonical_nodes (node_type, name, path, body)
VALUES
('component', 'orchestrator',         '/components/orchestrator',       '{"description":"Orchestrator CLI (Go). Submits tasks, waits for completion. INSERT tasks + PG NOTIFY tasks_assigned. CLI-only in pilot — no daemon.","language":"Go","entrypoint":"cmd/orchestrator/main.go","cli":"orchestrator submit --soul <id> --title <text> --wait"}'::jsonb),
('component', 'pool-controller',      '/components/pool-controller',    '{"description":"Pool Controller (Go). Agent registry (in-memory + DB), heartbeat monitoring (Redis Pub/Sub subscribe), task routing (random agent per soul), backpressure tracking, dead agent reaping. HTTP :8301 /health.","language":"Go","entrypoint":"cmd/pool-controller/main.go","http_port":8301}'::jsonb),
('component', 'policy-engine',        '/components/policy-engine',      '{"description":"Policy Engine (Go). Evaluates allow/deny/review rules per task. Writes audit_log + human_reviews. Soul sheet watcher for policy updates.","language":"Go","entrypoint":"cmd/policy-engine/main.go"}'::jsonb),
('component', 'recovery-controller',  '/components/recovery-controller','{"description":"Recovery Controller (Go). Monitors agent liveness via Redis Pub/Sub. Detects dead agents (> 30s no heartbeat). Re-queues abandoned tasks → PENDING. Idempotency ledger guards duplicate actions. Checkpoint replay deferred until agent-side checkpointing exists. HTTP :8302 /health.","language":"Go","entrypoint":"cmd/recovery-controller/main.go","http_port":8302}'::jsonb),
('component', 'eval-aggregator',      '/components/eval-aggregator',    '{"description":"Eval Aggregator (Go). Subscribes to eval_record PG channel. Maintains 20-task DriftWindow per soul_id. Inserts evaluation_records + drift_snapshots (every 60s). Alerts when rolling mean < 0.6. HTTP :8303 /health.","language":"Go","entrypoint":"cmd/eval-aggregator/main.go","http_port":8303}'::jsonb),
('component', 'memory-controller',    '/components/memory-controller',  '{"description":"Memory Controller (Go). HTTP API :8300. POST /assemble assembles context variables from canonical_nodes, embeddings, and session_store. Manages pgvector HNSW index.","language":"Go","entrypoint":"cmd/memory-controller/main.go","http_port":8300}'::jsonb)
ON CONFLICT (name, node_type) DO NOTHING;

-- ============================================================================
-- 4. Agent layer components (Python)
-- ============================================================================
INSERT INTO canonical_nodes (node_type, name, path, body)
VALUES
('component', 'agent-runtime',        '/components/agent-runtime',      '{"description":"Agent Runtime (Python). Stateful daemon: IDLE→WARMING→ACTIVE→IDLE. Polls tasks via SELECT FOR UPDATE SKIP LOCKED. Renders prompts via chevron (Mustache/Handlebars). Calls LLM Gateway via GatewayClient. Writes results to tasks table. Publishes heartbeats to Redis Pub/Sub every 5s.","language":"Python","entrypoint":"rasa/agent/runtime.py","template_engine":"chevron"}'::jsonb),
('component', 'llm-gateway',          '/components/llm-gateway',        '{"description":"LLM Gateway (Python). Tier routing (standard/premium). Redis SHA-256 prompt cache. Seed bypass for non-deterministic responses. Fallback chain: same-tier→degrade→OpenAI (last resort). Model resolution via RASA_DEFAULT_MODEL / RASA_PREMIUM_MODEL env vars with ${VAR:-default} expansion.","language":"Python","entrypoint":"rasa/llm_gateway/","models":["Deepseek-v4-flash:cloud","Deepseek-v4-pro:cloud"],"fallback":"OpenAI API via FALLBACK_API_KEY"}'::jsonb),
('component', 'sandbox-pipeline',     '/components/sandbox-pipeline',    '{"description":"Sandbox Pipeline (Python). 6-gate state machine: IDLE→CLONING→SCANNING→BUILDING→TESTING→PROMOTING→CLEANUP. Regex-based secret scanning (6 rules: AWS keys, private keys, API keys, tokens, passwords, connection strings). Temp-directory subprocess jail with timeouts. Upgrade path: gVisor + Semgrep.","language":"Python","entrypoint":"rasa/sandbox/","scanner":"regex-6-rules","sandbox":"temp-directory-subprocess-jail"}'::jsonb),
('component', 'eval-scorer',          '/components/eval-scorer',         '{"description":"Eval Scorer (Python). One-shot CLI: scores completed tasks 0-1 on structural heuristics (content length, model field, usage info, structure quality, error absence). Publishes eval_record to PG channel.","language":"Python","entrypoint":"rasa/eval/scorer.py"}'::jsonb),
('component', 'observe-dashboard',    '/components/observe-dashboard',   '{"description":"observe.py — Live terminal dashboard (Python). Queries SQL views across 5 databases every 30s. Prints task summary, agent states, soul performance, drift status, backpressure events, recovery actions, policy decisions. Pure stdout — no web UI.","language":"Python","entrypoint":"scripts/observe.py","refresh_interval_seconds":30}'::jsonb)
ON CONFLICT (name, node_type) DO NOTHING;

-- ============================================================================
-- 5. Soul nodes (agents)
-- ============================================================================
INSERT INTO canonical_nodes (node_type, name, path, body)
VALUES
('soul', 'coder-v2-dev',    '/souls/coder',    '{"description":"Primary coder agent. Role: CODER. Standard tier (Deepseek-v4-flash:cloud). Temperature: 0.2, max_tokens: 8192. Handles code generation, refactoring, bug fixes.","role":"CODER","tier":"standard","soul_file":"souls/coder-v2-dev.yaml"}'::jsonb),
('soul', 'reviewer-v1',     '/souls/reviewer', '{"description":"Code reviewer agent. Role: REVIEWER. Reviews code for quality, security, and policy compliance.","role":"REVIEWER","tier":"standard","soul_file":"souls/reviewer-v1.yaml"}'::jsonb),
('soul', 'planner-v1',      '/souls/planner',  '{"description":"Task planner agent. Role: PLANNER. Premium tier (Deepseek-v4-pro:cloud). Decomposes goals into task sequences.","role":"PLANNER","tier":"premium","soul_file":"souls/planner-v1.yaml"}'::jsonb),
('soul', 'architect-v1',    '/souls/architect','{"description":"System architect agent. Role: ARCHITECT. Premium tier (Deepseek-v4-pro:cloud). Designs system architecture and reviews cross-cutting concerns.","role":"ARCHITECT","tier":"premium","soul_file":"souls/architect-v1.yaml"}'::jsonb)
ON CONFLICT (name, node_type) DO NOTHING;

-- ============================================================================
-- 6. Concept / decision nodes (lore)
-- ============================================================================
INSERT INTO canonical_nodes (node_type, name, path, body)
VALUES
('concept', 'task-state-machine',           '/concepts/task-states',           '{"description":"Task lifecycle: PENDING→ASSIGNED→RUNNING→COMPLETED/FAILED. Recovery: ASSIGNED→PENDING (re-queue on agent death). FAILED→PENDING (manual retry). RUNNING→CHECKPOINTED (multi-turn daemon only).","states":["PENDING","ASSIGNED","RUNNING","COMPLETED","FAILED","CHECKPOINTED"]}'::jsonb),
('concept', 'message-duality',              '/concepts/message-duality',       '{"description":"Two-tier messaging: (1) PostgreSQL LISTEN/NOTIFY + backing tables for durable events (tasks, checkpoints, sandbox results, eval records). (2) Redis Pub/Sub for ephemeral events (heartbeats every 5s, policy updates). No NATS — removed from architecture.","durable":"PG LISTEN/NOTIFY","ephemeral":"Redis Pub/Sub","removed":"NATS/JetStream"}'::jsonb),
('concept', 'soul-sheets',                  '/concepts/soul-sheets',           '{"description":"Agent personality defined in YAML files (souls/*.yaml). Fields: metadata, agent_role, model (tier, temperature, max_tokens), behavior (session, tool_policy), prompt (system_template, context_injection). Rendered via chevron (Python Mustache/Handlebars). Validated via JSON Schema draft 2020-12. Inheritance: parent-child merge, child overrides, arrays replaced.","format":"YAML 1.2","validation":"JSON Schema draft 2020-12","template_engine":"chevron (Mustache/Handlebars)","inheritance":"parent-child merge"}'::jsonb),
('concept', 'pilot-scope',                  '/concepts/pilot-scope',           '{"description":"Pilot tradeoffs — deferred upgrades: gRPC (currently JSON/localhost), gVisor sandbox (subprocess jail), Semgrep (regex scanner), Docker Compose/K8s (Procfile), MinIO/S3 (flat files), Redis Cluster (single-node), Apache AGE (JSONB graph). All architecture boundaries deployment-agnostic.","current":"native Windows, Procfile, JSON over localhost, subprocess jail, regex scan, flat files, single-node Redis, JSONB","upgrade":"K8s, gRPC, gVisor, Semgrep, S3, Redis Cluster, Apache AGE"}'::jsonb),
('concept', 'decision-log',                 '/concepts/decisions',             '{"description":"Key decisions: (1) PostgreSQL as sole bus — zero new infra deps. (2) Go for control plane (goroutines/channels for state machines), Python for agent layer (LLM SDK ecosystem). (3) Claude Code IS the orchestrator during pilot — delegates, never executes. (4) 6 separate databases for clean domain boundaries. (5) Environment variables for model selection (RASA_DEFAULT_MODEL, RASA_PREMIUM_MODEL).","decisions":[{"date":"2026-04-23","decision":"PG as sole bus","reason":"no extra infra deps at pilot scale"},{"date":"2026-04-28","decision":"Redisc Pub/Sub for ephemeral","reason":"heartbeats/policy are loss-tolerant"},{"date":"2026-04-28","decision":"Drop NATS entirely","reason":"PG+Redis cover all patterns"},{"date":"2026-04-29","decision":"DB-backed metrics","reason":"durable,queryable,already in stack"}]}'::jsonb)
ON CONFLICT (name, node_type) DO NOTHING;

-- ============================================================================
-- 7. Wire up outgoing edges (references from each node to related nodes)
-- ============================================================================
-- Helper: we update each row with its outgoing UUID array after all inserts.

-- rasa-platform → everything system-level
UPDATE canonical_nodes SET outgoing_edges = ARRAY(
    SELECT id FROM canonical_nodes WHERE name IN (
        'postgresql', 'redis', 'ollama-cloud', 'honcho',
        'rasa_orch', 'rasa_pool', 'rasa_policy', 'rasa_memory', 'rasa_eval', 'rasa_recovery',
        'orchestrator', 'pool-controller', 'policy-engine', 'recovery-controller', 'eval-aggregator', 'memory-controller',
        'agent-runtime', 'llm-gateway', 'sandbox-pipeline', 'eval-scorer', 'observe-dashboard',
        'coder-v2-dev', 'reviewer-v1', 'planner-v1', 'architect-v1',
        'task-state-machine', 'message-duality', 'soul-sheets', 'pilot-scope', 'decision-log'
    )
) WHERE name = 'rasa-platform' AND node_type = 'system';

-- postgresql → all 6 databases
UPDATE canonical_nodes SET outgoing_edges = ARRAY(
    SELECT id FROM canonical_nodes WHERE node_type = 'database'
) WHERE name = 'postgresql' AND node_type = 'infra';

-- redis → ephemeral consumers
UPDATE canonical_nodes SET outgoing_edges = ARRAY(
    SELECT id FROM canonical_nodes WHERE name IN (
        'pool-controller', 'recovery-controller', 'agent-runtime', 'llm-gateway', 'memory-controller'
    )
) WHERE name = 'redis' AND node_type = 'infra';

-- ollama-cloud → LLM gateway
UPDATE canonical_nodes SET outgoing_edges = ARRAY(
    SELECT id FROM canonical_nodes WHERE name = 'llm-gateway'
) WHERE name = 'ollama-cloud' AND node_type = 'infra';

-- orchestrator → databases + downstream
UPDATE canonical_nodes SET outgoing_edges = ARRAY(
    SELECT id FROM canonical_nodes WHERE name IN (
        'rasa_orch', 'rasa_pool', 'pool-controller', 'task-state-machine', 'message-duality'
    )
) WHERE name = 'orchestrator' AND node_type = 'component';

-- pool-controller → agents + databases
UPDATE canonical_nodes SET outgoing_edges = ARRAY(
    SELECT id FROM canonical_nodes WHERE name IN (
        'rasa_orch', 'rasa_pool', 'agent-runtime', 'agent-runtime', 'message-duality', 'redis'
    )
) WHERE name = 'pool-controller' AND node_type = 'component';

-- agent-runtime → souls + gateway + memory
UPDATE canonical_nodes SET outgoing_edges = ARRAY(
    SELECT id FROM canonical_nodes WHERE name IN (
        'llm-gateway', 'memory-controller', 'rasa_orch', 'redis',
        'coder-v2-dev', 'reviewer-v1', 'planner-v1', 'architect-v1',
        'soul-sheets', 'chevron'
    )
) WHERE name = 'agent-runtime' AND node_type = 'component';

-- llm-gateway → ollama + cache
UPDATE canonical_nodes SET outgoing_edges = ARRAY(
    SELECT id FROM canonical_nodes WHERE name IN ('ollama-cloud', 'redis')
) WHERE name = 'llm-gateway' AND node_type = 'component';

-- memory-controller → its database
UPDATE canonical_nodes SET outgoing_edges = ARRAY(
    SELECT id FROM canonical_nodes WHERE name = 'rasa_memory'
) WHERE name = 'memory-controller' AND node_type = 'component';

-- sandbox-pipeline → scanner + orch
UPDATE canonical_nodes SET outgoing_edges = ARRAY(
    SELECT id FROM canonical_nodes WHERE name IN ('rasa_orch', 'pilot-scope')
) WHERE name = 'sandbox-pipeline' AND node_type = 'component';

-- recovery-controller → orch + recovery DB + Redis
UPDATE canonical_nodes SET outgoing_edges = ARRAY(
    SELECT id FROM canonical_nodes WHERE name IN ('rasa_orch', 'rasa_recovery', 'redis', 'task-state-machine')
) WHERE name = 'recovery-controller' AND node_type = 'component';

-- eval-aggregator → eval DB + scorer
UPDATE canonical_nodes SET outgoing_edges = ARRAY(
    SELECT id FROM canonical_nodes WHERE name IN ('rasa_eval', 'eval-scorer', 'observe-dashboard')
) WHERE name = 'eval-aggregator' AND node_type = 'component';

-- Souls → their tier infra
UPDATE canonical_nodes SET outgoing_edges = ARRAY(
    SELECT id FROM canonical_nodes WHERE name IN ('llm-gateway', 'agent-runtime')
) WHERE node_type = 'soul';

-- Concepts → each other
UPDATE canonical_nodes SET outgoing_edges = ARRAY(
    SELECT id FROM canonical_nodes WHERE node_type = 'concept'
) WHERE node_type = 'concept' AND name = 'decision-log';

UPDATE canonical_nodes SET outgoing_edges = ARRAY(
    SELECT id FROM canonical_nodes WHERE name IN ('task-state-machine', 'message-duality', 'soul-sheets', 'pilot-scope')
) WHERE node_type = 'concept' AND name = 'decision-log';

-- ============================================================================
-- 8. Insert orchestrator soul sheet into soul_sheets table
-- ============================================================================
INSERT INTO soul_sheets (soul_id, version, agent_role, body, source_path)
VALUES (
    'hermes-v1',
    '1.0.0',
    'ORCHESTRATOR',
    '{
        "soul_id": "hermes-v1",
        "metadata": {
            "name": "Hermes",
            "description": "RASA orchestrator — Claude Code instance that delegates work to agents. Never executes directly.",
            "created_at": "2026-04-23",
            "version": "1.0.0",
            "tags": ["orchestrator", "control-plane", "claude-code"]
        },
        "agent_role": "ORCHESTRATOR",
        "model": {
            "default_tier": "premium",
            "temperature": 0.2,
            "max_tokens": 8192
        },
        "behavior": {
            "session": {
                "mode": "session-based",
                "heartbeat_interval_seconds": null
            },
            "tool_policy": {
                "allowed_tools": ["submit-task", "query-db", "read-file", "commit-code", "delegate"],
                "denied_tools": ["execute-code-directly", "modify-production-data"],
                "require_human_confirm": ["delete-database", "force-push"]
            }
        },
        "prompt": {
            "system_template": "You are Hermes, the RASA orchestrator. you delegate work to specialized agents via PostgreSQL. You maintain project state in .hermes/AGENTS.md and .hermes/SOUL.md. You commit code after each work block. You never execute agent work directly — always dispatch.",
            "context_injection": "Current phase: Pilot. All 5 implementation gates complete. 4 coder agents running, 1 reviewer, 1 planner, 1 architect. PostgreSQL is the sole message bus. Redis for heartbeats only."
        },
        "responsibilities": [
            "Read project state from .hermes/AGENTS.md and .hermes/SOUL.md",
            "Decide what work needs to happen next",
            "INSERT tasks into rasa_orch.tasks with status PENDING",
            "PG NOTIFY tasks_assigned for Pool Controller to route",
            "Wait for completion via task_completed channel or polling",
            "Review results and decide next steps",
            "Commit code changes after each work block",
            "Maintain AGENTS.md with current project state"
        ]
    }'::jsonb,
    '.hermes/SOUL.md'
) ON CONFLICT (soul_id) DO UPDATE SET
    body = EXCLUDED.body,
    updated_at = NOW();

-- ============================================================================
-- 9. Verify
-- ============================================================================
SELECT node_type, COUNT(*) AS count FROM canonical_nodes GROUP BY node_type ORDER BY count DESC;
SELECT soul_id, agent_role FROM soul_sheets;
