# RASA Orchestrator Setup

## Overview

Claude Code serves as the Hermes orchestrator for RASA. It manages the system architecture, maintains project context, and delegates work to Python agents. Everything runs natively on a single Windows 11 machine.

## Context Files

Claude Code discovers persistent context from these sources:

### 1. `.hermes/SOUL.md` (per-user, survives across sessions)
Contains hardware specs, preferences (aggressive cleanup, `mv` over `cp`), PostgreSQL connection details, and model preferences. Injected into every turn, subject to ~2200 char cap for personal notes and ~1375 char cap for user profile. Use sparingly for stable facts.

### 2. `AGENTS.md` (repo root, committed)
Per-repo orchestrator instructions. Used as persistent state for the project's current phase, file layout, conventions, and blockers. Survives context window truncation much better than session memory.

### 3. `CLAUDE.md` (repo root)
Tooling guidance for Claude Code — commands, architecture, conventions, pitfalls.

### 4. `.hermes/AGENTS.md` (inside repo)
Full system state, architecture decisions, decision log, known issues.

## Discovery Order at Session Start

1. Read `.hermes/SOUL.md` into personal memory
2. Inject session-specific context (current working dir, system prompt)
3. Traverse project directory for `AGENTS.md`, `CLAUDE.md`
4. Truncate subdirectory context if too deep (risk: silently eats ~20k tokens)
5. Load `.hermes/memories/*.md` into user profile (~1375 char cap)

## Orchestrator Responsibilities

In the RASA architecture, Claude Code is the brain, not the hands:

- **Read files** in `rasa/` via Read, Grep, Glob
- **Create tasks** by inserting rows into `rasa_orch.tasks`
- **Query PostgreSQL** for task status, audit logs, heartbeats
- **Route LLM requests** through `rasa/llm_gateway/client.py`
- **Commit often**: `git add <files> && git commit -m "..."` after every coherent change

## What the Orchestrator Does NOT Do

- Execute agent work directly — always dispatch via task creation
- Hold long-lived state across sessions — use PostgreSQL or AGENTS.md
- Write agent code directly unless trivial (the pool controller and dispatcher handle that)

## Execution Pattern

```bash
# Direct agent dispatch (one-shot)
python -m rasa.agent.dispatcher --soul coder-v2-dev --goal "Refactor DB layer" --one-shot

# Pool controller (long-running poller using PG LISTEN/NOTIFY + Redis heartbeats)
python -m rasa.pool.controller --pool-file config/pool.yaml

# Task creation (for pool controller to pick up)
psql -U postgres -d rasa_orch -c "INSERT INTO tasks (title, status, soul_id) VALUES ('...', 'PENDING', 'coder-v2-dev');"
```

## Command Reference

```bash
# Run pool controller
python -m rasa.pool.controller --pool-file config/pool.yaml

# Check task status
psql -U postgres -d rasa_orch -c "SELECT status, COUNT(*) FROM tasks GROUP BY status;"

# Run a dispatcher directly (testing)
python -m rasa.agent.dispatcher --soul coder-v2-dev --goal "Test" --dry-run

# Build Go control plane
go build ./cmd/...

# Start all services
honcho start
```
