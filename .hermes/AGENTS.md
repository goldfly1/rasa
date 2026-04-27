# RASA Project Context

## Mission
RASA (Reliable Autonomous System of Agents) is a distributed multi-agent orchestration platform. The WSL-side orchestrator (Hermes/kimi-k2.6) delegates tasks to Windows-side workers (coding agents, ComfyUI, QA/testing, generic processes).

## Architecture Overview
```
WSL (Orchestrator)          Windows (Workers)
  Hermes/kimi-k2.6    <--->   Python 3.13 Agents
  PostgreSQL (6 DBs)  <--->   ComfyUI / Claude Code
  Redis (hot state)     <--->   PowerShell / Go binaries
  Go 1.26.2 (planned)
```

## Core Design Decisions
- **PostgreSQL as primary backplane**: Durable task state in `rasa_orch` database. NATS is NOT required (skipped due to winget redirect issues). Redis used for hot state/caching only.
- **Hybrid execution**: Orchestrator logic in Python (immediate), control-plane services in Go (future phase).
- **Windows-native Python**: Using Windows Python 3.13.6 at `C:\Users\goldf\AppData\Local\Programs\Python\Python313\python.exe` because ComfyUI and GPU tools live on Windows.
- **Soul sheets**: YAML configs defining agent personalities, model tiers, tool policies, CLI bindings. Located in `souls/`.

## Current Phase: Phase 1 (Active)
**Goal**: Working LLM Gateway + Agent Dispatcher + Pool Controller
- ✅ Rasa Python package v0.1.0 installed in `C:\Users\goldf\rasa\.venv`
- ✅ LLM Gateway client (`rasa/llm_gateway/client.py`) — Ollama API, tier routing, caching to `rasa_memory`
- ✅ Agent Dispatcher (`rasa/agent/dispatcher.py`) — reads soul sheets, calls LLM, writes to DB
- ✅ Pool Controller (`rasa/pool/controller.py`) — WSL-side poller that spawns Windows workers
- ✅ Database layer (`rasa/db/conn.py`) — psycopg ConnectionPool, DSN builder
- ✅ AGENTS.md orchestrator context file
- 🔄 Next: Policy Engine (`rasa/policy/engine.py`) — rule evaluation, audit logging
- ⏳ Phase 2: Agent Runtime + actual worker spawning
- ⏳ Phase 3: Sandbox Pipeline (semgrep, pytest, build)
- ⏳ Phase 4: Evaluation + Observability

## Active Files (check these first)
| File | Purpose | Phase |
|------|---------|-------|
| `rasa/llm_gateway/client.py` | `GatewayClient.complete()` — async LLM calls | 1 |
| `rasa/agent/dispatcher.py` | `run_task()`, `daemon_loop()` — agent entrypoint | 1 |
| `rasa/pool/controller.py` | `PoolController` — task polling + Windows spawn | 1 |
| `rasa/db/conn.py` | PostgreSQL connection pool wrapper | 1 |
| `config/gateway.yaml` | LLM routing: ollama → gemma4:31b-cloud / kimi-k2.6:cloud | 0 |
| `config/pool.yaml` | Agent sizing: 2 coders, 1 reviewer, 1 planner, 1 architect | 0 |
| `souls/*.yaml` | 4 soul sheet definitions (coder, reviewer, planner, architect) | 0 |
| `migrations/010_rasa_orch.sql` | tasks, task_dependencies, checkpoint_refs tables | 0 |
| `migrations/020_rasa_pool.sql` | agents, heartbeats, backpressure_events tables | 0 |
| `migrations/030_rasa_policy.sql` | policy_rules, audit_log, human_reviews tables | 0 |
| `migrations/040_rasa_memory.sql` | canonical_nodes, embeddings, soul_sheets tables | 0 |
| `Procfile` | Service definitions (commented out NATS) | 0 |
| `scripts/setup_windows.ps1` | Prereq installer (Go, Redis skipped by user) | 0 |

## Environment Variables
```
RASA_DB_HOST=localhost
RASA_DB_PORT=5432
RASA_DB_USER=postgres
RASA_DB_PASSWORD=<redacted>
RASA_DB_NAME=rasa_orch
OLLAMA_BASE_URL=http://127.0.0.1:11434/v1
RASA_DEFAULT_MODEL=gemma4:31b-cloud
RASA_PREMIUM_MODEL=kimi-k2.6:cloud
```

## Key Commands
**Windows-side Python execution from WSL:**
```bash
powershell.exe -Command "C:\Users\goldf\rasa\.venv\Scripts\python.exe -m rasa.agent.dispatcher --soul coder-v2-dev --goal 'Refactor DB layer' --dry-run"
```

**Pool controller (WSL-side):**
```bash
python -m rasa.pool.controller --pool-file config/pool.yaml
```

**Database status check:**
```bash
psql -U postgres -d rasa_orch -c "SELECT status, COUNT(*) FROM tasks GROUP BY status;"
```

## Conventions
- **WSL→Windows bridge**: Always use `powershell.exe -Command` for Windows execution. Double-quote nesting often fails; prefer single-quote outer wrapping or file-based execution.
- **Path mapping**: Windows `C:\Users\goldf\rasa` ↔ WSL `/mnt/c/Users/goldf/rasa`
- **Python venv**: `C:\Users\goldf\rasa\.venv\Scripts\python.exe`
- **Pip self-upgrade**: Must use `python -m pip install --upgrade pip` inside venv, never direct `pip.exe` invocation.
- **Soul YAML**: Uses `chevron` (Mustache) templating. Context dict has keys: `metadata`, `agent_role`, `task`, `memory`, `tools`.
- **DB migrations**: Apply via `scripts/bootstrap_schema.ps1` or direct `psql -f migrations/010_rasa_orch.sql`
- **Git**: Commit early and often. Working tree is usually clean after commits.
- **Storage**: Aggressive cleanup (SSD ~250GB). Use `mv` not `cp` for large files. Delete source after moving.

## Known Issues / Pitfalls
1. **WSL→Windows PG connection refused**: Direct `psql` from WSL fails. Use PowerShell passthrough:
   ```bash
   powershell.exe -Command "\$env:PGPASSWORD='PASSWORD'; & 'C:\Program Files\PostgreSQL\18\bin\psql.exe' -U postgres -h localhost -d rasa_orch -c 'QUERY'"
   ```
2. **NATS not installed**: Skipped entirely. No plans to add unless user explicitly requests.
3. **Go binaries not compiled**: Stubs exist in `cmd/*/main.go` but no `go build` yet.
4. **Redis server not running**: Client library installed but `redis-server` may not be launched.
5. **PowerShell quote escaping**: Double quotes inside `-Command` often fail. Use `-File` with `.ps1` scripts instead.
6. **Context window limits**: Hermes memory caps at ~2200 chars (personal) + ~1375 chars (user profile). Use AGENTS.md for persistent context instead of relying on memory.
7. **Subdirectory hints silently eat tokens**: Avoid `find` or deep `ls` in large repos without filtering.

## Decision Log
- **2026-04-25**: Phase 0 scaffold committed (architecture docs, 6 DBs, migrations).
- **2026-04-26**: Phase 0.1 — Python venv, all deps installed, `rasa==0.1.0` verified.
- **2026-04-26**: Phase 0.2 — Go 1.26.2 and Redis 3.0.504 confirmed installed by user. NATS skipped.
- **2026-04-27**: Phase 1 started — LLM Gateway, Agent Dispatcher, Pool Controller, DB layer committed.
- **Pending**: Policy Engine implementation, actual task execution smoke test.
