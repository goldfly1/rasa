# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

RASA (Reliable Autonomous System of Agents) is a multi-agent orchestration platform running on a single-node lab machine. This Claude Code instance serves as the orchestrator (Hermes), delegating work to Python agents and Go control-plane services — all running natively on Windows.

- **Repo**: https://github.com/goldfly1/rasa
- **Hardware**: Intel Ultra 7 255, 64GB RAM, RTX 5060 8GB, 1TB SSD (~250GB free)
- **Stack**: Go 1.24+ (control plane), Python 3.12+ (agent runtime/LLM gateway), PostgreSQL 16+ (6 databases), Redis, Ollama
- **Current phase**: Phase 1 — All 5 implementation gates complete (pilot scaffolded end-to-end)

## Architecture

Single-machine, everything on Windows. PostgreSQL is the sole communications backbone during pilot — no NATS, no external message broker.

```
Claude Code (orchestrator)     Python agents (venv)
  └── AGENTS.md context         └── soul sheets → LLM → DB write
         │                              ▲
         └── PostgreSQL ◄───────────────┘
                ▲
         Go control plane (stubs)
```

- **PostgreSQL as bus**: `rasa_orch.tasks` is the job queue. Durable task state, LISTEN/NOTIFY for wake-up, no separate broker needed at pilot scale.
- **Redis**: Hot state only — heartbeats, session cache, Pub/Sub for ephemeral policy updates.
- **Task state machine**: `PENDING → ASSIGNED → RUNNING → CHECKPOINTED/COMPLETED/FAILED`.

## Commands

All commands run directly on Windows. The Python venv is at `.venv\Scripts\python.exe`.

### Python

```bash
# Install / upgrade
python -m pip install --upgrade pip
pip install -e ".[dev]"

# Lint / type-check
ruff check rasa/
mypy rasa/

# Run tests
pytest tests/ -v
pytest tests/ -v -k "test_name"

# One-shot agent dispatch
python -m rasa.agent.dispatcher --soul coder-v2-dev --goal "Refactor DB layer" --dry-run
python -m rasa.agent.dispatcher --soul coder-v2-dev --task-id <uuid> --one-shot

# Pool controller (polling loop)
python -m rasa.pool.controller --pool-file config/pool.yaml

# LLM Gateway standalone
python -m rasa.llm_gateway --config config/gateway.yaml

# Start all services
honcho start
honcho start <service>
```

### Go

```bash
go build ./cmd/...
go build -o orchestrator.exe ./cmd/orchestrator/
./orchestrator.exe --db "postgres://localhost/rasa_orch?sslmode=disable"
```

### Database

```bash
# Bootstrap
.\scripts\create_databases.ps1
.\scripts\bootstrap_schema.ps1

# Ad-hoc queries
psql -U postgres -d rasa_orch -c "SELECT status, COUNT(*) FROM tasks GROUP BY status;"
psql -U postgres -d rasa_orch -f migrations/010_rasa_orch.sql
```

## Repository Layout

| Directory | Purpose |
|-----------|---------|
| `rasa/` | Python package — agent runtime, LLM gateway, pool controller, DB layer |
| `cmd/*/main.go` | Go service stubs: orchestrator, pool-controller, policy-engine, recovery-controller, eval-aggregator, memory-controller |
| `internal/` | Go shared packages: db, config, models, natsutil (legacy envelope types) |
| `config/` | gateway.yaml, pool.yaml |
| `souls/` | Agent soul sheets (YAML): coder, reviewer, planner, architect |
| `migrations/` | PostgreSQL DDL for all 6 databases |
| `scripts/` | PowerShell helpers: setup, database creation, schema bootstrap |
| `tests/` | Integration tests, smoke tests |
| `docs/` | Architecture docs, setup guides |
| `schema/implementation/` | Per-component implementation specs |
| `.hermes/` | Orchestrator context files (AGENTS.md, SOUL.md) |

## Key Architecture Decisions

- **PostgreSQL as sole bus (pilot)**: LISTEN/NOTIFY for task wake-up, backing tables for durability. Redis Pub/Sub only for loss-tolerant ephemeral messages (heartbeats, policy change notifications). No NATS — the operational complexity of a third infrastructure service isn't warranted at pilot scale.
- **Soul sheets**: YAML files defining agent personality and model routing. Template syntax is Mustache/Handlebars rendered via the `chevron` library in the Python Agent Runtime. The legacy dispatcher.py uses Handlebars→Jinja2 regex translation (lossy); new code should use `runtime.py` with chevron.
- **Subprocess-based workers**: Each agent is a separate process spawned via `subprocess.Popen(start_new_session=True)`. Workers are fire-and-forget and survive controller restarts.
- **Persistent orchestrator context**: `.hermes/AGENTS.md` and repo-root `AGENTS.md` carry project state across sessions. These are the authoritative source — Claude's memory is too small (~3600 chars).
- **6 PostgreSQL databases**: `rasa_orch`, `rasa_pool`, `rasa_policy`, `rasa_memory`, `rasa_eval`, `rasa_recovery` — each owns its domain with clear boundaries.

## Conventions

- Pip self-upgrade: Always `python -m pip install --upgrade pip` inside venv, never direct `pip.exe`.
- Soul rendering context dict: `metadata`, `agent_role`, `model`, `behavior`, `tools`, `task`, `memory`.
- Git: Commit early and often. Working tree should be clean after each work block.
- Disk space: Use `mv` not `cp` for large files; delete sources after moving.
- Environment: `RASA_DB_PASSWORD` set via `.env` or OS environment, never hardcoded.

## Known Pitfalls

1. The legacy dispatcher's Handlebars→Jinja2 regex translation is lossy — only `{{#each}}` is supported. Prefer `runtime.py` which uses `chevron` for proper Mustache/Handlebars rendering.
2. Go stubs in `cmd/` no longer reference `--nats`. NATS was removed from the architecture; all messaging is PG LISTEN/NOTIFY + Redis Pub/Sub.
3. `GatewayClient.__init__` creates a new cache pool (`get_pool("rasa_memory")`) on every instantiation. Known leak, deferred for now.
4. `.hermes/AGENTS.md` and older docs still reference WSL and powershell.exe bridging patterns — those are stale. Everything runs on Windows directly.
