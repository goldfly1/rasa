# RASA Orchestrator / Hermes Setup (WSL)

## Overview

The Hermes orchestrator runs inside WSL (Windows Subsystem for Linux), manages the system architecture, and delegates work to Windows-side Python agents. This document describes how the orchestrator is configured, how it discovers project context, and how it dispatches tasks.

## Context Files

Hermes discovers persistent context from three sources:

### 1. `~/.hermes/SOUL.md` (per-user, survives across sessions)
Contains your hardware specs, preferences (aggressive cleanup, `mv` over `cp`), PostgreSQL connection details, ComfyUI paths, and model preferences. This is injected into every turn, subject to ~2200 char cap for personal notes and ~1375 char cap for user profile. Use it sparingly for stable facts.

### 2. Project-level `AGENTS.md` (repo root, committed)
Per-repo orchestrator instructions. Hermes discovers `AGENTS.md` at `/mnt/c/Users/goldf/rasa/AGENTS.md` and uses it as persistent state for the project's current phase, file layout, conventions, and blockers. This survives context window truncation much better than session memory.

### 3. `.hermes/SOUL.md` inside the repo (optional, not yet created)
A repo-local SOUL for team-wide context. Currently not needed since AGENTS.md covers the orchestrator scope.

## Discovery Order at Session Start

When a new Hermes session begins with working directory `/mnt/c/Users/goldf/rasa`:

1. Read `~/.hermes/config.yaml` (model provider, memory db URL)
2. Load `~/.hermes/SOUL.md` into personal memory
3. Inject session-specific context (current working dir, system prompt)
4. Traverse project directory for `AGENTS.md`, `CLAUDE.md`, `.cursorrules`, `SOUL.md`
5. Truncate subdirectory context if too deep (risk: silently eats ~20k tokens)
6. Load `~/.hermes/memories/*.md` into user profile (~1375 char cap)

## Orchestrator Responsibilities

In the RASA architecture, Hermes is the brain, not the hands:

- **Read files** in `rasa/` via `read_file`, `search_files`, `terminal`
- **Delegate work** via `delegation_tools` to Windows workers
- **Query PostgreSQL** for task status, audit logs, heartbeats
- **Route LLM requests** through `rasa/llm_gateway/client.py`
- **Commit often**: `git add -A && git commit -m "..."` after every coherent change

## What Hermes Does NOT Do

- Run Windows executables directly from WSL (use `powershell.exe` bridge)
- Control Windows-side agents inline (always dispatch, don't run)
- Hold long-lived state across sessions (use PostgreSQL or AGENTS.md)
- Code directly unless trivial (<5 lines of boilerplate)

## WSL-to-Windows Dispatch Pattern

```bash
# Bad: runs in terminal() background-detector
powershell.exe -Command "& 'C:\Users\goldf\.venv\Scripts\python.exe' ..."

# Good: write a .ps1, then execute it
powershell.exe -File C:\Users\goldf\rasa\scripts\run_task.ps1
```

The pool controller (`rasa/pool/controller.py`) does exactly this: polls `rasa_orch.tasks` from WSL, finds `PENDING` rows, writes a `.ps1` script with the password env var, then spawns it via `subprocess.Popen(..., start_new_session=True)`.

## Command Reference

```bash
# Run pool controller from WSL
python -m rasa.pool.controller --pool-file config/pool.yaml

# Check task status
scripts/check_tasks.ps1   # or direct psql via PowerShell

# Run a dispatcher directly (testing)  
powershell.exe -File scripts/smoke_dispatcher.ps1
```
