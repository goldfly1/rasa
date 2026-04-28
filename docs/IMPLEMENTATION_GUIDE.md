# RASA Implementation Guide

## 1. Orchestrator (Claude Code)

### 1.1 What the Orchestrator Does
Claude Code (this instance) serves as the orchestrator. It reads the project state, decides what needs to happen next, and **dispatches** — never executes directly. The orchestrator:
- Maintains long-term project memory via `.hermes/AGENTS.md` and `.hermes/SOUL.md`
- Reads/writes PostgreSQL in the `rasa_orch` database
- Spawns Windows-side workers via `subprocess.Popen(start_new_session=True)`
- Commits code changes after each work block

### 1.2 Persistent Context
| File | Size | Contents |
|------|------|----------|
| `.hermes/SOUL.md` | ~1.2 kB | Orchestrator role, constraints, current phase, invocation style |
| `.hermes/AGENTS.md` | ~6.3 kB | Full mission, architecture, decisions, pitfalls, commands |
| `AGENTS.md` (repo root) | ~2.8 kB | Same content, used if `.hermes/` missing |
| `CLAUDE.md` (repo root) | ~2.5 kB | Tooling guidance for Claude Code |

These files survive across sessions. Claude Code discovers and loads them automatically.

### 1.3 Memory Caps
- Personal memory: ~2200 chars
- User profile: ~1375 chars
- **These are not enough** for full project state. Use `AGENTS.md` + filesystem instead.
- Subdirectory scans of large repos silently eat tokens — avoid unfiltered `find` or `ls -R`.

### 1.4 Execution Patterns

**Pattern A: One-shot Python agent**
```bash
python -m rasa.agent.dispatcher --soul coder-v2-dev --goal "Refactor DB layer" --one-shot
```

**Pattern B: Pool controller loop**
```bash
python -m rasa.pool.controller --pool-file config/pool.yaml
```
This polls `rasa_orch.tasks` via PostgreSQL LISTEN/NOTIFY and spawns Pattern A as needed.

**Pattern C: Direct PostgreSQL task creation**
```sql
INSERT INTO rasa_orch.tasks (title, description, payload, status, soul_id)
VALUES ('Refactor DB layer', '...', '{"type": "refactor"}', 'PENDING', 'coder-v2-dev');
```

---

## 2. Workers (Python Agents)

### 2.1 Worker Architecture
Every worker is an instance of `rasa.agent.dispatcher`. It:
1. Reads its **soul sheet** (`souls/*.yaml`)
2. Renders the **system prompt** via Jinja2 (Handlebars `{{#each}}` translated to Jinja2 `{% for %}`)
3. Calls the **LLM** via Ollama's OpenAI-compatible API (localhost:11434)
4. Writes the **result** back to PostgreSQL (`rasa_orch.tasks`)
5. Optionally writes a **checkpoint file** to `checkpoints/<task_id>.json`

### 2.2 Soul Sheets
Soul sheets (YAML files in `souls/`) define:
- `agent_role`: CODER, REVIEWER, PLANNER, ARCHITECT
- `model`: tier (standard/premium), temperature, max_tokens
- `prompt.system_template`: Jinja2-ish template with `{{agent_role}}`, `{{#each metadata.tags}}`
- `prompt.context_injection`: extra context block appended after system prompt
- `behavior.tool_policy`: allowed/denied tools, human-confirm triggers
- `behavior.session.mode`: `one-shot` or `daemon`
- `cli.argument_binding`: `--task-id` maps to `task.id`, etc.

### 2.3 Jinja2 Rendering Pipeline
Soul sheets use **Handlebars** (`{{#each}}`) because the original design targeted Go's `raymond` library. The Python dispatcher translates Handlebars to Jinja2 at runtime:

```python
def hb_to_jinja(text):
    text = re.sub(r'\{\{#each\s+([\w.]+)\s*\}\}', r'{% for item in \1 %}', text)
    text = text.replace('{{this}}', '{{item}}')
    text = re.sub(r'\{\{/each\s*\}\}', '{% endfor %}', text)
    return text
```

### 2.4 LLM Gateway
Both tiers run through the local Ollama gateway at `http://127.0.0.1:11434/v1`:
- **Standard tier** → `gemma4:31b-cloud`
- **Premium tier** → `kimi-k2.6:cloud`

Configuration in `config/gateway.yaml`. The `rasa/llm_gateway/client.py` module provides an async client with retry (3 attempts, exponential backoff) and best-effort response caching to `rasa_memory`.

### 2.5 Database Writes
After the LLM call completes, the dispatcher updates `rasa_orch.tasks`:
```sql
UPDATE tasks
SET status = 'COMPLETED',
    completed_at = NOW(),
    result = '{"content": "...", "model": "..."}'::jsonb
WHERE id = '<task_id>';
```

For daemon-mode agents, status is `CHECKPOINTED` and a checkpoint ref is inserted:
```sql
INSERT INTO checkpoint_refs (task_id, agent_id, snapshot_path, metadata)
VALUES ('<task_id>', 'agent-coder-v2-dev', 'checkpoints/<task_id>.json', '{"turn": 1}');
```

### 2.6 Environment Variables
| Variable | Default | Purpose |
|----------|---------|---------|
| `RASA_DB_PASSWORD` | *(required)* | PostgreSQL password |
| `RASA_DB_HOST` | `localhost` | Database host |
| `RASA_DB_PORT` | `5432` | Database port |
| `RASA_DB_USER` | `postgres` | Database user |
| `RASA_DEFAULT_MODEL` | `Deepseek-v4-flash:cloud` | Standard tier LLM |
| `RASA_PREMIUM_MODEL` | `Deepseek-v4-pro:cloud` | Premium tier LLM |
| `OLLAMA_BASE_URL` | `http://127.0.0.1:11434/v1` | Ollama gateway |
| `OLLAMA_API_KEY` | `ollama` | API key |

---

## 3. Pool Controller

### 3.1 What It Does
`rasa/pool/controller.py` polls for tasks and spawns workers:
1. Listens to PostgreSQL `tasks_assigned` channel via LISTEN/NOTIFY
2. On notification, fetches pending tasks from `rasa_orch.tasks`
3. Reads `config/pool.yaml` for soul→replica mapping
4. Spawns workers via `subprocess.Popen(start_new_session=True)`
5. Tracks heartbeats via Redis Pub/Sub (`agents.heartbeat.*`)

### 3.2 Why Subprocess Instead of Threads?
- Workers are **separate processes** with their own Python interpreter and soul state
- Each worker has a distinct `task_id` and writes to the DB independently
- `start_new_session=True` makes them survive the pool controller restarting

### 3.3 Pool YAML
```yaml
pool:
  max_agents: 8
  min_agents: 2
souls:
  - id: "coder-v2-dev"
    replicas: 2
  - id: "reviewer-v1"
    replicas: 1
```

---

## 4. Inter-Component Communication

All messaging uses infrastructure already in the stack — no separate message broker.

| Message Type | Backend | Rationale |
|-------------|---------|-----------|
| **Durable events** (task assignment, checkpoints, sandbox results) | PostgreSQL LISTEN/NOTIFY + backing tables | Transaction-safe, zero extra deps |
| **Ephemeral events** (heartbeats, policy updates) | Redis Pub/Sub | High-frequency, loss-tolerant |

### Channel Topology (PostgreSQL)
| Channel | Producer | Consumer | Backing Table |
|---------|----------|----------|---------------|
| `tasks_assigned` | Orchestrator | Pool Controller | `tasks` |
| `tasks_submit` | CLI | Orchestrator | `tasks` |
| `checkpoint_saved` | Agent Runtime | Recovery Controller | `checkpoints` |
| `sandbox_result` | Sandbox Pipeline | Orchestrator | `sandbox_results` |
| `eval_record` | Evaluation Engine | Orchestrator | `evaluation_records` |

### Channel Topology (Redis Pub/Sub)
| Channel | Producer | Consumer | Notes |
|---------|----------|----------|-------|
| `agents.heartbeat.{agent_id}` | Agent Runtime | Pool Controller | Glob: `agents.heartbeat.*` |
| `policy.update` | Policy Engine admin | Policy Engine instances | PG poll (30s) catches misses |

---

## 5. Data Flow (End-to-End)

```
1. Orchestrator decides a task is needed
   └── INSERT INTO rasa_orch.tasks (title, status='PENDING', soul_id='coder-v2-dev')

2. Pool Controller receives notification
   └── LISTEN tasks_assigned → NOTIFY triggers SELECT on pending tasks

3. Worker spawned via subprocess
   └── python -m rasa.agent.dispatcher --soul coder-v2-dev --task-id <uuid> --one-shot

4. Dispatcher executes
   a. Reads souls/coder-v2-dev.yaml
   b. Renders prompt with Jinja2 (Handlebars → Jinja2)
   c. Calls Ollama (gemma4:31b-cloud) via localhost HTTP
   d. UPDATE tasks SET status='COMPLETED', result='{...}' WHERE id=<uuid>

5. Orchestrator checks DB for done tasks
   └── SELECT id, status, result FROM tasks WHERE status='COMPLETED'
   └── Reviews result, decides next task or marks done
```

### 5.1 Failure Paths
| Scenario | Handling |
|----------|----------|
| Worker crashes before DB write | Heartbeat timeout (> 15s); task stays ASSIGNED, retry scheduled |
| LLM timeout | Retries 3× with exponential backoff; final status FAILED |
| Policy rule violation | Policy engine writes audit_log + human_reviews row; blocks commit |
| PostgreSQL unreachable | Dispatcher exits with error code 255; Pool Controller logs backpressure |
| Redis unavailable | Heartbeat silence triggers agent timeout; PG policy poll catches up |

---

## 6. Key Files

| File | Purpose |
|------|---------|
| `.hermes/SOUL.md` | Orchestrator context (Claude Code) |
| `.hermes/AGENTS.md` | Full system state + decisions |
| `CLAUDE.md` | Claude Code tooling guidance |
| `rasa/agent/dispatcher.py` | Worker: soul → prompt → LLM → DB |
| `rasa/pool/controller.py` | Polls tasks via PG LISTEN/NOTIFY, spawns workers |
| `rasa/llm_gateway/client.py` | Async Ollama client with tier routing |
| `rasa/db/conn.py` | psycopg connection pool |
| `souls/*.yaml` | Agent personality + model config |
| `config/gateway.yaml` | LLM provider + tier mapping |
| `config/pool.yaml` | Agent sizing + replica counts |
| `migrations/010_*.sql` | `rasa_orch` schema (tasks, deps, checkpoints) |
| `migrations/020_*.sql` | `rasa_pool` schema (agents, heartbeats, backpressure) |
| `migrations/030_*.sql` | `rasa_policy` schema (rules, audit, human reviews) |
| `migrations/040_*.sql` | `rasa_memory` schema (canonical nodes, embeddings, soul sheets) |
