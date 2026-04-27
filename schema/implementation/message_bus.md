# Message Bus

> **Architectural Reference:** `architectural_schema_v2.1.md` §4  
> **Implementation Reference:** [`agent_configuration.md`](agent_configuration.md) — filesystem watcher replaces NATS-based soul.update   
> **Status:** Draft — pilot provisioning  
> **Owner:** TBD  
> **Last Updated:** 2026-04-25

---

## 1. Purpose

Transports inter-agent envelopes. Guarantees ACK/NACK, dead-lettering on retry exhaustion, and watermark tracking for replay. Carries soul-aware metadata in every envelope to ensure routed messages can be correlated with agent identity and prompt versions.

---

## 2. Internal Design

### 2.1 Envelope Schema (soul-aware)

Every message on the bus includes a `metadata` block. For the pilot, envelopes are serialized as **JSON** over localhost (Protobuf is the documented upgrade path). The schema is identical in semantics:

```json
{
  "message_id": "uuid",
  "correlation_id": "uuid",
  "source_component": "orchestrator | pool_controller | agent_runtime | ...",
  "destination_component": "orchestrator | pool_controller | agent_runtime | ...",
  "payload": {} ,

  "metadata": {
    "soul_id": "coder-v2-dev",
    "prompt_version_hash": "a3f2...",
    "agent_role": "CODER",
    "task_id": "0195f...",
    "agent_id": "agent-42",
    "timestamp_ms": 1745620000000
  }
}
```

> **Upgrade path:** Replace JSON serialization with Protobuf code-generated bindings. The NATS Go and Python clients support both transparently through their `Encoder`/`Decoder` interfaces.

### 2.2 Topic Topology

| Topic | Producer | Consumer | Pilot Notes |
|-------|----------|----------|-------------|
| `tasks.assigned` | Orchestrator | Pool Controller | Route to agents with matching `soul_id` |
| `agents.heartbeat.{agent_id}` | Agent Runtime | Pool Controller | Tagged with `soul_id` for per-soul health metrics |
| `policy.update` | Policy Engine admin | Policy Engine instances | Push rule changes |
| `checkpoint.saved` | Agent Runtime | Recovery Controller | Includes `soul_id` and `prompt_version_hash` |
| `sandbox.result` | Sandbox Pipeline | Orchestrator, Observability | Tagged with `soul_id` for per-role gate analysis |
| `eval.record` | Evaluation Engine | Observability, Orchestrator | Tagged with `soul_id` and `prompt_version_hash` |
| `replay.trace` | LLM Gateway | Observability | Deterministic sampling traces |

> **Removed from pilot:** `souls.update` — replaced by filesystem watcher (`agent_configuration.md` §2.7). Restore this topic as part of the NATS-based hot-reload upgrade path.

### 2.3 Dead Letter Policy

- **Max retries:** 3 for all topics (pilot simplification; per-topic retry counts can be reintroduced later).
- **Dead-letter retention:** 7 days.
- **Dead-letter consumer:** Observability Stack alerts on any message entering the DLQ.

---

## 3. Tech Stack Choices

| Concern | Selection | Rationale |
|---------|-----------|-----------|
| **Broker** | **NATS JetStream** (native binary, single-node) | Lighter than Kafka; stronger durability than Redis Pub/Sub. Matches architectural specification. |
| **Serialization** | **JSON** (pilot); **Protobuf** (upgrade) | Avoids codegen overhead during rapid iteration. NATS supports both through encoder plugins. |
| **Go client** | `github.com/nats-io/nats.go` | Official NATS Go client; supports JetStream, KV store, Object Store. |
| **Python client** | `nats-py` | Official NATS Python client; async-native (asyncio), supports JetStream. |
| **Config format** | `nats-server.conf` (YAML-style) | Single-file config; loaded at startup by the native binary. |

---

## 4. Deployment Topology

- **Process:** Single NATS server process, started via Procfile entry:
  ```
  nats: nats-server -c <project_root>/config/nats-server.conf
  ```
- **Data directory:** `<data_root>/nats/data/` — JetStream streams, message stores.
- **Ports:**
  - `4222` — Client connections
  - `8222` — HTTP monitoring endpoint (health checks, metrics)
- **Auth:** None (pilot — localhost only). Add NATS JWT-based auth when exposing beyond localhost.
- **Storage limits:** JetStream file store with a per-stream storage budget (see §5).

---

## 5. Operational Concerns

| Metric | Pilot Action | Upgrade Path |
|--------|--------------|--------------|
| Disk space (JetStream storage) | Set `max_store` per stream; total budget of 5 GB for pilot | Scale with NATS clustering and tiered storage |
| Message throughput | Monitor via NATS HTTP endpoint (`localhost:8222`) | Prometheus metrics exporter |
| Consumer lag | Not monitored in pilot (single-node, low volume) | Consumer lag tracking via JetStream API |
| Connection count | Logged on connect/disconnect | Prometheus gauge + alert |
| Dead-letter queue volume | Manual `nats CLI` inspection | Automated alert on DLQ > 0 |

---

## 6. Open Questions

| # | Question | Status |
|---|----------|--------|
| 1 | Bus ordered within a thread or partitioned by topic? | **Resolved:** Partitioned by topic. Order within a topic guaranteed by JetStream. |
| 2 | Dead-letter retention policy? | **Resolved:** 7 days for pilot. |
| 3 | Should `souls.update` be a priority topic? | **Obsolete:** Replaced by filesystem watcher. |
| 4 | What is the JetStream per-stream storage budget, and how is it allocated across streams? | **Open:** Recommend 5 GB total across all streams for pilot; revisit when replay bundles accumulate. |

---

## 7. Change Log

| Date | Change | Author |
|------|--------|--------|
| 2026-04-25 | Pilot provisioning: replaced Protobuf with JSON envelope, removed `souls.update` topic (filesystem watcher), added native-binary deployment, filled Tech Stack / Deployment / Operational sections. | Codex |
| 2026-04-25 | Added soul-aware envelope schema and topic topology | ? |

---

*This document implements the protocol contract defined in `architectural_schema_v2.1.md` §4. Envelope schema aligns with `agent_configuration.md` §2.2 metadata fields.*
