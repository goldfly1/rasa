# Observability Stack

> **Architectural Reference:** `architectural_schema_v2.1.md` §7  
> **Implementation Reference:** [`agent_configuration.md`](agent_configuration.md) — soul_id and prompt_version_hash tagging   
> **Status:** Draft — pilot provisioning  
> **Owner:** TBD  
> **Last Updated:** 2026-04-25

---

## 1. Purpose

Captures reasoning traces, tool telemetry, diffs, and metrics. Tags every signal with `soul_id` and `prompt_version_hash` to enable per-personality debugging and prompt regression analysis. In the pilot, all observability is built on **structured JSON logs to stdout** — no OTel collector, no Grafana. Logs can be piped to a file, viewed in real-time, or queried with standard CLI tools.

---

## 2. Internal Design

### 2.1 Trace Schema (soul-aware)

Every component writes a JSON line per significant event. The schema fields are identical to the enterprise OTel specification — only the transport changes:

```json
{
  "level": "info",
  "timestamp": "2026-04-25T12:00:00.000Z",
  "component": "agent_runtime",
  "event": "SESSION_START",
  "soul.id": "coder-v2-dev",
  "soul.role": "CODER",
  "soul.prompt_hash": "a3f2...",
  "soul.model_tier": "standard",
  "task.id": "0195f...",
  "agent.id": "agent-42",
  "message": "Agent session started"
}
```

**Event categories emitted by each component:**

| Component | Events |
|-----------|--------|
| Agent Runtime | `SESSION_START`, `SESSION_END`, `HEARTBEAT`, `TOOL_CALL`, `TOOL_RESULT`, `CHECKPOINT_SAVED` |
| LLM Gateway | `MODEL_REQUEST`, `CACHE_HIT`, `CACHE_MISS`, `FALLBACK_TRIGGERED`, `BUDGET_EXHAUSTED` |
| Orchestrator | `TASK_PENDING`, `TASK_ASSIGNED`, `TASK_COMPLETE`, `TASK_ESCALATED`, `RETRY_SCHEDULED` |
| Pool Controller | `AGENT_REGISTERED`, `HEARTBEAT_MISS`, `BACKPRESSURE_ACTIVE`, `DRAINING` |
| Policy Engine | `POLICY_ALLOW`, `POLICY_DENY`, `POLICY_REQUIRE_REVIEW` |
| Sandbox Pipeline | `SANDBOX_START`, `SCAN_PASS`, `BUILD_PASS`, `TEST_PASS`, `GATE_FAILURE`, `PROMOTED` |
| Recovery Controller | `RECOVERY_START`, `RECOVERY_SUCCESS`, `RECOVERY_FAILED`, `PROMPT_DRIFT` |
| Evaluation Engine | `BENCHMARK_START`, `BENCHMARK_COMPLETE`, `DRIFT_ALERT`, `PROMOTION_BLOCKED` |
| Memory Subsystem | `CONTEXT_ASSEMBLED`, `EMBEDDING_STORED`, `EVICTION_RUN`, `RECONCILER_DIFF` |

### 2.2 Replay Bundles

A replay bundle is an immutable artifact at `<data_root>/replays/{task_id}/` capturing a complete agent session:

```
replay-{task_id}-{timestamp}/
  soul_sheet.yaml          # Exact soul sheet used
  prompt_template.txt      # Assembled prompt before variable substitution
  prompt_final.txt         # Final prompt sent to LLM Gateway
  reasoning_trace.jsonl    # Streaming tool calls + LLM streamed completions
  memory_context.json      # Snapshot of Memory Subsystem output
  policy_decisions.json    # Policy Engine allow/deny log
  sandbox_results.json     # Scanner + test gate output
  metadata.json            # task_id, soul_id, prompt_version_hash, model_id, token_count
```

Replay bundles are referenced by `checkpoint_id` in PostgreSQL. They are compressed with gzip after 24 hours.

### 2.3 Query Patterns (Pilot Dashboards)

Without Grafana, dashboards become CLI query patterns against aggregate log files. Examples:

```powershell
# Global: error rate per component
Get-Content logs/*.log | ConvertFrom-Json | Where-Object { $_.level -eq "error" } | Group-Object component

# Per-soul pass rate
Get-Content logs/*.log | ConvertFrom-Json | Where-Object { $_.event -eq "GATE_FAILURE" -or $_.event -eq "PROMOTED" } | Group-Object "soul.id"

# Per-prompt-hash latency
Get-Content logs/*.log | ConvertFrom-Json | Where-Object { $_.event -eq "TASK_COMPLETE" } | Select-Object "soul.prompt_hash", "task.id", "timestamp"

# Budget burn per model tier
Get-Content logs/*.log | ConvertFrom-Json | Where-Object { $_.event -eq "MODEL_REQUEST" } | Group-Object "soul.model_tier"
```

> **Upgrade path:** Replace these CLI queries with Grafana dashboards powered by OpenTelemetry + Prometheus/Loki/Tempo. The log schema stays the same — only the storage backend changes.

---

## 3. Tech Stack Choices

| Concern | Selection | Rationale |
|---------|-----------|-----------|
| **Log format** | **JSON lines** (newline-delimited JSON) | Machine-parseable, human-readable, standard `jq`/`ConvertFrom-Json` compatibility. |
| **Log output** | **stdout** per component process | Zero infrastructure. Procfile captures output; can be redirected to a file or pipe. |
| **Log sink** | **Rotating file** (`logs/rasa-{component}-{date}.log`) | Simple file rotation to prevent disk exhaustion. 7-day retention (no TTL enforcement in pilot). |
| **Replay bundle storage** | **Flat files on disk** (`<data_root>/replays/`) | Same as archive storage. Referenced by checkpoint ID in PostgreSQL. |
| **KPI rollup** | **Python script** (`scripts/observe.py`) | End-of-day batch reads log files, produces summary stats, writes report to `logs/reports/`. |

---

## 4. Deployment Topology

- **Log collection:** Each component writes JSON lines to stdout. The Procfile runner (or a wrapper script) redirects stdout to a rotating file:
  ```
  # Procfile
  logs: python scripts/observe.py --watch logs/ --interval 60  # Optional live viewer
  ```
- **Log directory:** `<project_root>/logs/{component}-{date}.log` — one file per component per day.
- **Replay bundle directory:** `<project_root>/data/replays/{task_id>/` — written by Agent Runtime on CHECKPOINTED.
- **Report directory:** `<project_root>/logs/reports/` — KPI rollups from the end-of-day script.
- **No external services:** No OTel collector, no Prometheus, no Grafana. Everything lives on the local filesystem.

---

## 5. Operational Concerns

| Metric | Pilot Action | Concern |
|--------|--------------|---------|
| Log disk usage | 7-day rolling rotation | `~10 MB/day per component` at pilot scale — negligible for 250 GB free |
| Replay bundle disk usage | Per task, cleaned up manually | `~1-5 MB per task` — monitor total in `<data_root>/replays/` |
| Log parsing performance | `Get-Content` queries on 7-day files | Fine for pilot (< 100 MB total logs). Structured logging tool (e.g., `lnav`) can be introduced later |
| KPI report accuracy | End-of-day batch may miss in-flight tasks | Acceptable for pilot — reports reflect completed tasks only |

---

## 6. Open Questions

| # | Question | Status |
|---|----------|--------|
| 1 | How long are raw reasoning traces retained? | **Resolved:** 7 days on disk. No TTL enforcement in pilot — delete manually or add a cleanup script. |
| 2 | What is the aggregation window for KPI rollups? | **Resolved:** End-of-day batch script for pilot. Real-time aggregation (Prometheus) is the upgrade path. |
| 3 | Should replay bundles include the full LLM response stream or just the final completion? | **Resolved:** Full streamed response. More useful for debugging prompt issues. |

---

## 7. Change Log

| Date | Change | Author |
|------|--------|--------|
| 2026-04-25 | Pilot provisioning: replaced OTel/Prometheus/Grafana stack with structured JSON logs to stdout, replaced S3 replay bundle storage with local flat files, added CLI query patterns for pilot dashboards, added per-component event catalog, filled Tech Stack / Deployment / Operational sections. | Codex |
| 2026-04-25 | Added soul-aware trace schema, replay bundle structure, and dashboard organization | ? |

---

*This document implements the observability contract defined in `architectural_schema_v2.1.md` §7. Trace schema aligns with `agent_configuration.md` §2.2 soul metadata fields.*
