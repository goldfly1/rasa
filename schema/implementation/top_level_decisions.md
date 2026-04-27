# Top-Level Decisions

> **Architectural Reference:** `architectural_schema_v2.1.md` (cross-cutting)
> **Status:** Draft — pilot provisioning
> **Hardware Environment:** Omen 16 — Intel Ultra 7 255, 64 GB RAM, RTX 5060 8 GB VRAM, 1 TB SSD (~250 GB free). PostgreSQL 16 + pgvector installed locally. Single-node lab machine.
> **Owner:** TBD
> **Last Updated:** 2026-04-25

---

## 1. Purpose

Records global implementation choices that affect multiple components: programming languages, deployment platform, persistence strategy, message transport, container strategy, networking, and standards.

These decisions are **provisional by design**. Each entry includes a review date. When a decision changes, this document is updated first; downstream component docs follow.

---

## 2. Decisions

### 2.1 Languages

| Decision | Selection | Rationale | Review Date |
|----------|-----------|-----------|-------------|
| **Control Plane** | **Go 1.24+** | Goroutines/channels map cleanly to the Orchestrator, Pool Controller, Message Bus, and Recovery Controller. Static binaries simplify sidecar injection into agent pods. | 2026-07-23 |
| **Agent Runtime & LLM Gateway** | **Python 3.12+** | Ecosystem dominance for LLM SDKs (OpenAI, Anthropic, transformers). Agents require rapid iteration; Python minimizes glue code for model calls. | 2026-07-23 |
| **Inter-language contracts** | **Protobuf + gRPC** (pilot: plain JSON over localhost as interim) | Type-safe, code-generated bindings keep architecture entities honest. For the pilot, JSON over localhost is acceptable to avoid codegen overhead; gRPC can be introduced when components are deployed across hosts. | 2026-07-23 |

### 2.2 Persistence

| Decision | Selection | Rationale | Review Date |
|----------|-----------|-----------|-------------|
| **Primary durable store** | **PostgreSQL 16+** | ACID guarantees for the Task Lifecycle state machine, Idempotency Ledger, and Evaluation Records. JSONB accommodates polymorphic checkpoint metadata without schema migrations. | 2026-07-23 |
| **Vector index** | **pgvector** (PostgreSQL extension) | Single operational surface with the primary store. Simplifies backups, transactions, and access control. Acceptable latency trade-off for the current scale; a dedicated vector DB can be introduced later without changing the architecture boundary. | 2026-07-23 |
| **Session / hot-state cache** | **Redis** (single-node) | Sub-millisecond TTL-based heartbeats, warm pool pre-initialization, and ephemeral checkpoint buffers. Volatile by design; durability is PostgreSQL\'s job. Single-node for pilot; Redis Cluster is the documented upgrade path. | 2026-07-23 |
| **Graph store** | **JSONB columns + indexed foreign keys** (pilot) | The canonical model for a single-repo pilot fits comfortably in JSONB columns. Multi-hop graph traversal is not needed at this scale. Migrate to pg_graph / Apache AGE when the model outgrows a few JOINs. | 2026-07-23 |
| **Blob / snapshot store** | **Flat files on disk** (pilot: `<data_dir>/snapshots/`) | Agent session snapshots, replay bundles, and conversation archives stored as files under a project-local directory. Pointers stored in PostgreSQL `checkpoint_refs` table. Migrate to MinIO / S3 when cross-host access is required. | 2026-07-23 |

### 2.3 Message Transport

| Decision | Selection | Rationale | Review Date |
|----------|-----------|-----------|-------------|
| **Primary bus** | **NATS JetStream** (native binary, single-node) | Native streaming, durable replay, consumer groups, and dead-lettering. Runs as a native process via Procfile. Single-node for pilot; clustered JetStream can be introduced later. | 2026-07-23 |
| **Envelope format** | **Protobuf** | Schema evolution via optional fields; smaller wire size than JSON. Aligns with §4 Protocol Definitions. | 2026-07-23 |

### 2.4 Sandbox & Execution

| Decision | Selection | Rationale | Review Date |
|----------|-----------|-----------|-------------|
| **Sandbox runtime** | **Temp-directory subprocess jail** (pilot) | Each build/test runs in a disposable temp folder with a timeout and process-tree kill on failure. Proves the Sandbox Pipeline state machine without OS-level virtualization. gVisor (+ WSL2 or Linux host) is the documented upgrade path. | 2026-07-23 |
| **Scanner chain** | **Python** (Semgrep, detect-secrets, custom AST rules) | Reuse the agent-runtime language to share security rule libraries and reduce toolchain sprawl. | 2026-07-23 |

### 2.5 Deployment & Infrastructure

| Decision | Selection | Rationale | Review Date |
|----------|-----------|-----------|-------------|
| **Process management** | **Procfile** via `honcho` (Python) or `launch.ps1` | Single command starts all native processes (NATS, Redis, agent components). Ctrl-C shuts down the constellation. No orchestration dependency for the pilot. | 2026-07-23 |
| **Component lifecycle** | **Native binaries + Python venvs** | Go components built with `go build`, Python components run in a project-local virtual environment. No container images. | 2026-07-23 |
| **Observability** | **Structured JSON logs to stdout/stderr** (pilot); **OpenTelemetry → Prometheus/Grafana** as upgrade path | Distributed tracing adds infrastructure overhead. For the pilot, structured JSON logs captured from each component\'s stdout provide sufficient debugging capability. Migrate to OTel + Prometheus/Grafana when the agent fleet exceeds 3-4 concurrent agents. | 2026-07-23 |
| **Secret & config distribution** | **`.env` file** + OS environment variables (pilot) | Short-lived LLM API keys and database credentials loaded from a `.env` file in the project root, not checked into version control. Migrate to external-secrets / SPIFFE when deploying to a shared environment. | 2026-07-23 |

> **Upgrade Path:** The architecture boundaries are deployment-agnostic. When the pilot outgrows a single machine, the migration path is: native Procfile → Docker Compose → Kubernetes. No component requires K8s-specific APIs in its current design; all interfaces (NATS topics, PostgreSQL connections, gRPC endpoints) are host/port-addressable.

### 2.6 API Exposure

| Decision | Selection | Rationale | Review Date |
|----------|-----------|-----------|-------------|
| **Internal service mesh** | **gRPC** (pilot: JSON over localhost as interim) | Backpressure, streaming logs/traces, and codegen. Pilot uses plain JSON on localhost to avoid codegen overhead. gRPC enforced when components leave localhost. | 2026-07-23 |
| **External / human-facing** | **REST** via gRPC-Gateway | Operational dashboards and manual interventions use REST/JSON; generated from the same protobuf definitions to prevent drift. | 2026-07-23 |

### 2.7 Agent Configuration & Prompting

| Decision | Selection | Rationale | Review Date |
|----------|-----------|-----------|-------------|
| **Soul sheet format** | **YAML 1.2** | Human-readable, diff-friendly in Git, widely understood by ops and dev teams. | 2026-07-23 |
| **Schema validation** | **JSON Schema** (draft 2020-12) | Standard, well-understood, generates clear error messages at load time. | 2026-07-23 |
| **Template engine** | **Handlebars** | Logic-light, deterministic output (same inputs = same hash), portable across Go and Python. Go implementation (`github.com/aymerick/raymond`) aligns with Control Plane language. | 2026-07-23 |
| **Inheritance resolution** | **Parent-child merge** (child overrides) | DRY principle for soul sheets; base `coder` soul, specialized `coder-python` child. Arrays replaced, not appended. | 2026-07-23 |
| **Soul storage (source of truth)** | **Git** | History, code review, and branching for soul sheet changes. | 2026-07-23 |
| **Soul storage (runtime)** | **PostgreSQL** (cache) + **flat files** (blobs > 1MB) | PostgreSQL provides fast lookup by `soul_id`; local flat files handle large embedded prompt examples. Migrate to S3 when deploying across hosts. | 2026-07-23 |
| **Hot reload** | **Filesystem watcher** (pilot); **NATS** `souls.update` topic (upgrade) | On a native install with soul sheets as local YAML files, a filesystem watcher (`fsnotify` in Go, `watchdog` in Python) is simpler than a NATS topic for hot-reload. Migrate to NATS-based reload when soul sheets live in a database and multiple processes need coordinated updates. | 2026-07-23 |

---

## 3. Decision Impact Matrix

| Component | Primary Language | Key Persistence | Transport |
|-----------|------------------|-----------------|-----------|
| Orchestrator | Go | PostgreSQL | NATS JetStream |
| Pool Controller | Go | PostgreSQL + Redis | NATS JetStream |
| Recovery Controller | Go | PostgreSQL + flat files | NATS JetStream |
| Agent Runtime | Python | Redis + PostgreSQL | NATS JetStream / local IPC |
| Agent Configuration | Go (validator), Python (loader) | PostgreSQL + flat files + Git | Filesystem watcher |
| LLM Gateway | Python | Redis (cache) | NATS JetStream |
| Sandbox Pipeline | Python | Flat files (artifacts) | NATS JetStream |
| Memory Subsystem | Go (controller), Python (embedder) | PostgreSQL + pgvector + Redis | NATS JetStream |
| Message Bus | Go | NATS JetStream (durability) | NATS JetStream |
| Observability Stack | Go | JSON log files (pilot); Prometheus/Loki/Tempo (upgrade) | NATS JetStream |
| Evaluation Engine | Go (aggregator), Python (scorer) | PostgreSQL | NATS JetStream |
| Policy Engine | Go | PostgreSQL (rules) | NATS JetStream |

---

## 4. Open Questions

| # | Question | Impact | Target Resolution |
|---|----------|--------|-------------------|
| 1 | Single-node PostgreSQL 16. **Resolved** — local instance, no replication. | — | — |
| 2 | NATS JetStream as native binary, single-node. **Resolved** — runs via Procfile. | — | — |
| 3 | Flat files on disk for blob storage. **Resolved** — MinIO/S3 as upgrade path. | — | — |
| 4 | Warm pool = native processes, statically sized. **Resolved** — 2-3 agent processes for pilot. K8s DaemonSet/Deployment as upgrade. | — | — |
| 5 | pgvector index type: HNSW vs. IVFFlat — which index strategy for anticipated embedding volume? | Memory Subsystem | 2026-05-15 |
| 6 | What is the maximum tolerable cold-start time for bootstrap ingestion on a ~50K-100K LOC repo? | Bootstrap & Ingestion | 2026-05-15 |
| 7 | Should the pilot enforce a per-soul budget quota in addition to the global ceiling? | Policy Engine, LLM Gateway | 2026-05-15 |

---

## 5. Change Log

| Date | Change | Author |
|------|--------|--------|
| 2026-04-25 | Pilot provisioning: replaced K8s/gVisor/S3/AGE decisions with native-process equivalents (Procfile, temp-dir sandbox, flat-file blob, JSONB graph). Added hardware environment header. Updated open questions. | Codex |
| 2026-04-23 | Initial populated draft from architectural_schema_v2.1.md | ? |
| 2026-04-23 | Added Kubernetes escape hatch (§2.5) | ? |
| 2026-04-25 | Added Agent Configuration & Prompting decisions (§2.7); updated Decision Impact Matrix with Agent Configuration component | ? |

---

*This document is the single source of truth for cross-cutting implementation decisions. If a component doc contradicts this file, this file wins until formally revised.*
