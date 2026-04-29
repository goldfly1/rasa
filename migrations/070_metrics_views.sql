-- Migration 070: Metrics & Observability Views
-- Creates queryable views across RASA databases for the observe.py dashboard.
-- Views are per-database; observe.py joins results in Python.
--
-- Run: psql -U postgres -f migrations/070_metrics_views.sql

-- ============================================================================
-- rasa_orch views
-- ============================================================================

\c rasa_orch;

-- Task latency breakdown: queue, exec, and total time per task ----------------
CREATE OR REPLACE VIEW v_task_latency AS
SELECT
    id AS task_id,
    soul_id,
    status,
    created_at,
    assigned_at,
    started_at,
    completed_at,
    failed_at,
    EXTRACT(epoch FROM (assigned_at - created_at))      AS queue_seconds,
    EXTRACT(epoch FROM (started_at - assigned_at))       AS pickup_seconds,
    EXTRACT(epoch FROM (completed_at - started_at))      AS exec_seconds,
    EXTRACT(epoch FROM (COALESCE(completed_at, failed_at) - created_at)) AS total_seconds,
    retry_count
FROM tasks
WHERE created_at > NOW() - INTERVAL '30 days';

-- Daily task summary (last 30 days) -------------------------------------------
CREATE OR REPLACE VIEW v_daily_summary AS
SELECT
    DATE(created_at) AS day,
    COUNT(*) FILTER (WHERE status = 'COMPLETED') AS completed,
    COUNT(*) FILTER (WHERE status = 'FAILED')    AS failed,
    COUNT(*) FILTER (WHERE status = 'PENDING')   AS pending,
    COUNT(*) FILTER (WHERE status IN ('ASSIGNED', 'RUNNING')) AS in_flight,
    COUNT(*)                                       AS total,
    ROUND(
        AVG(EXTRACT(epoch FROM (completed_at - created_at)))
        FILTER (WHERE status = 'COMPLETED')
    )::BIGINT AS avg_latency_seconds
FROM tasks
WHERE created_at > NOW() - INTERVAL '30 days'
GROUP BY DATE(created_at)
ORDER BY day DESC;

-- ============================================================================
-- rasa_eval views
-- ============================================================================

\c rasa_eval;

-- Soul performance: avg score, pass rate, task count (last 7 days) ------------
CREATE OR REPLACE VIEW v_soul_performance AS
SELECT
    soul_id,
    COUNT(*)                                      AS task_count,
    ROUND(AVG(score), 4)                         AS avg_score,
    ROUND(AVG((metadata->>'passed')::boolean::int), 4) AS pass_rate,
    ROUND(AVG((metadata->>'duration_ms')::numeric), 0) AS avg_duration_ms,
    COUNT(*) FILTER (WHERE score < 0.5)          AS low_score_count
FROM evaluation_records
WHERE created_at > NOW() - INTERVAL '7 days'
GROUP BY soul_id
ORDER BY avg_score ASC;

-- Latest drift snapshots per soul ---------------------------------------------
CREATE OR REPLACE VIEW v_latest_drift AS
SELECT DISTINCT ON (soul_id)
    soul_id,
    window_size,
    mean_score,
    std_score,
    flagged,
    created_at
FROM drift_snapshots
ORDER BY soul_id, created_at DESC;

-- ============================================================================
-- rasa_pool views
-- ============================================================================

\c rasa_pool;

-- Agent uptime: heartbeat coverage per agent (gaps = downtime) -----------------
CREATE OR REPLACE VIEW v_agent_uptime AS
SELECT
    agent_id,
    MIN(received_at)                                   AS first_seen,
    MAX(received_at)                                   AS last_seen,
    COUNT(*)                                           AS heartbeat_count,
    EXTRACT(epoch FROM (MAX(received_at) - MIN(received_at))) AS span_seconds,
    CASE
        WHEN EXTRACT(epoch FROM (NOW() - MAX(received_at))) > 30
        THEN 'UNRESPONSIVE'
        ELSE 'ACTIVE'
    END                                                AS liveness
FROM heartbeats
WHERE received_at > NOW() - INTERVAL '1 day'
GROUP BY agent_id;

-- Pool saturation: backpressure events in last hour ---------------------------
CREATE OR REPLACE VIEW v_recent_backpressure AS
SELECT
    reason,
    agents_busy,
    agents_idle,
    triggered_at
FROM backpressure_events
WHERE triggered_at > NOW() - INTERVAL '1 hour'
ORDER BY triggered_at DESC;

-- ============================================================================
-- rasa_policy view
-- ============================================================================

\c rasa_policy;

-- Recent policy decisions (last 24h) ------------------------------------------
CREATE OR REPLACE VIEW v_recent_decisions AS
SELECT
    decision,
    COUNT(*) AS count,
    DATE_TRUNC('hour', created_at) AS hour
FROM audit_log
WHERE created_at > NOW() - INTERVAL '24 hours'
GROUP BY decision, DATE_TRUNC('hour', created_at)
ORDER BY hour DESC;

-- ============================================================================
-- rasa_recovery view
-- ============================================================================

\c rasa_recovery;

-- Recent recovery actions (last 24h) ------------------------------------------
CREATE OR REPLACE VIEW v_recent_recoveries AS
SELECT
    action,
    COUNT(*) AS count,
    DATE_TRUNC('hour', created_at) AS hour
FROM recovery_log
WHERE created_at > NOW() - INTERVAL '24 hours'
GROUP BY action, DATE_TRUNC('hour', created_at)
ORDER BY hour DESC;
