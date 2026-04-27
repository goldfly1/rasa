# RASA Implementation Guide

## 1. Orchestrator in WSL (Hermes)

### 1.1 What the Orchestrator Does
Hermes (this agent) lives in WSL. It reads the project state, decides what needs to happen next, and **dispatches** — never executes directly. The orchestrator:
- Maintains long-term project memory via `.hermes/AGENTS.md` and `.hermes/SOUL.md`
- Reads/writes PostgreSQL in the `rasa_orch` database
- Spawns Windows-side workers via `powershell.exe` passthrough
- Commits code changes after each work block

### 1.2 Why WSL?
- Your Windows filesystem is at `/mnt/c/`
- Direct `psql` from WSL to Windows PostgreSQL **fails** (connection refused)
- You must bridge through `powershell.exe -Command` for Windows-side execution
- The orchestrator handles the quote-escaping and cross-OS coordination

### 1.3 Persistent Context
| File | Size | Contents |
|------|------|----------|
| `.hermes/SOUL.md` | ~1.2 kB | Orchestrator role, constraints, current phase, invocation style |
| `.hermes/AGENTS.md` | ~6.3 kB | Full mission, architecture, decisions, pitfalls, commands |
| `AGENTS.md` (repo root) | ~2.8 kB | Same content, used if `.hermes/` missing |

These files survive across sessions. Hermes will discover them and load them automatically.

### 1.4 Memory Caps
- Personal memory: ~2200 chars
- User profile: ~1375 chars
- **These are not enough** for full project state. Use `AGENTS.md` + filesystem instead.
- Subdirectory scans of large repos silently eat tokens — avoid unfiltered `find` or `ls -R`.

### 1.5 WSL→Windows Execution Patterns

**Pattern A: One-shot Python script**
```bash
powershell.exe -Command "& 'C:\Users\goldf\rasa\.venv\Scripts\python.exe' -m rasa.agent.dispatcher --soul coder-v2-dev --goal 'Refactor DB layer' --one-shot"
```
Note: `--one-shot` is the default; only pass `--daemon` for long-lived agents.

**Pattern B: PowerShell script file** (avoids quote escaping)
```powershell
# tests/smoke_dispatcher.ps1
$env:RASA_DB_PASSWORD = '8764'
& "C:\Users\goldf\rasa\.venv\Scripts\python.exe" -m rasa.agent.dispatcher --soul coder-v2-dev --goal "Refactor DB layer" --dry-run --one-shot
```
Run from WSL:
```bash
powershell.exe -File tests/smoke_dispatcher.ps1
```

**Pattern C: Pool controller loop (WSL-side Python)**
```bash
python -m rasa.pool.controller --pool-file config/pool.yaml
```
This polls `rasa_orch.tasks` every 2 seconds and spawns Pattern A or B as needed.

---

## 2. Windows-Side Workers (Python)

### 2.1 Worker Architecture
Every worker is an instance of `rasa.agent.dispatcher`. It:
1. Reads its **soul sheet** (`souls/*.yaml`)
2. Renders the **system prompt** via Jinja2 (Handlebars `{{#each}}` translated to Jinja2 `{% for %}`)
3. Calls the **LLM** through `rasa.llm_gateway.client.GatewayClient`
4. Writes the **result** back to PostgreSQL (`rasa_orch.tasks`)
5. Optionally writes a **checkpoint file** to `checkpoints/<task_id>.json`

### 2.2 Soul Sheets
Soul sheets YAML files in `souls/` define:
- `agent_role`: CODER, REVIEWER, PLANNER, ARCHITECT
- `model`: tier (standard/premium), temperature, max_tokens
- `prompt.system_template`: Jinja2-ish template with `{{agent_role}}`, `{{#each metadata.tags}}`
- `prompt.context_injection`: extra context block appended after system prompt
- `behavior.tool_policy`: allowed/denied tools, human-confirm triggers
- `behavior.session.mode`: `one-shot` or `daemon`
- `cli.argument_binding`: `--task-id` maps to `task.id`, etc.

**Example: coder-v2-dev**
```yaml
soul_id: "coder-v2-dev"
agent_role: CODER
model:
  default_tier: "standard"
  temperature: 0.2
  max_tokens: 8192
prompt:
  system_template: |
    You are {{metadata.name}}, a {{agent_role}} agent in RASA.
    Specialties: {{#each metadata.tags}}{{this}}, {{/each}}
  context_injection: |
    Current task: {{task.title}}
    Codebase context: {{memory.short_term_summary}}
behavior:
  tool_policy:
    allowed_tools: [file_read, file_write, shell_exec, git_diff]
    denied_tools: [shell_exec:sudo, file_write:/etc/*]
```

### 2.3 Jinja2 Rendering Pipeline
The soul sheets use **Handlebars** (`{{#each}}`) because the original design targeted Go's `raymond` library. The Python dispatcher translates Handlebars to Jinja2 at runtime:

```python
# rasa/agent/dispatcher.py

def _render_system_prompt(soul, task, memory):
    import jinja2

    def hb_to_jinja(text):
        """Translate Handlebars {{#each path}} to Jinja2 {% for item in path %}."""
        text = re.sub(r'\{\{#each\s+([\w.]+)\s*\}\}', r'{% for item in \1 %}', text)
        text = text.replace('{{this}}', '{{item}}')
        text = re.sub(r'\{\{/each\s*\}\}', '{% endfor %}', text)
        return text

    env = jinja2.Environment(undefined=jinja2.StrictUndefined)
    ctx = {"metadata": soul["metadata"], "agent_role": soul["agent_role"], ...}
    return env.from_string(hb_to_jinja(soul["prompt"]["system_template"])).render(ctx)
```

### 2.4 LLM Gateway (`rasa/llm_gateway/client.py`)
```python
client = GatewayClient()
result = await client.complete(
    prompt="Refactor the DB layer",
    tier="standard",          # resolves to gemma4:31b-cloud
    # tier="premium",         # resolves to kimi-k2.6:cloud
)
# Returns: {"content": "...", "model": "gemma4:31b-cloud", "usage": {...}}
```

The gateway reads `config/gateway.yaml`:
```yaml
tiers:
  standard: {provider: ollama, model: gemma4:31b-cloud}
  premium:  {provider: ollama, model: kimi-k2.6:cloud}
```

Both are served by your local Ollama gateway at `http://127.0.0.1:11434/v1`.

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

### 2.6 Environment Variables (Windows-side)
These must be set in the PowerShell / Windows environment:
| Variable | Default | Purpose |
|----------|---------|---------|
| `RASA_DB_PASSWORD` | *(required)* | PostgreSQL password |
| `RASA_DB_HOST` | `localhost` | Database host |
| `RASA_DB_PORT` | `5432` | Database port |
| `RASA_DB_USER` | `postgres` | Database user |
| `RASA_DEFAULT_MODEL` | `gemma4:31b-cloud` | Standard tier LLM |
| `RASA_PREMIUM_MODEL` | `kimi-k2.6:cloud` | Premium tier LLM |
| `OLLAMA_BASE_URL` | `http://127.0.0.1:11434/v1` | Ollama gateway |
| `OLLAMA_API_KEY` | `ollama` | API key |

---

## 3. Pool Controller (WSL-side Python)

### 3.1 What It Does
`rasa/pool/controller.py` runs in WSL, **not** Windows. It:
1. Polls `rasa_orch.tasks` for `PENDING` tasks with no `assigned_agent_id`
2. Reads `config/pool.yaml` for soul→replica mapping
3. Spawns Windows-side workers via `subprocess.Popen(..., start_new_session=True)`
4. Logs spawn events to `rasa_pool.backpressure_events`

### 3.2 Why Subprocess Instead of Threads?
- Windows workers are **separate processes** with their own Python interpreter and soul state
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
The pool controller respects `max_agents` and tracks active workers in `rasa_pool.agents`.

---

## 4. Data Flow (End-to-End)

```
1. Hermes (WSL) decides a task is needed
   └── INSERT INTO rasa_orch.tasks (title, status='PENDING', soul_id='coder-v2-dev')

2. Pool Controller (WSL) polls tasks every 2s
   └── SELECT WHERE status='PENDING' AND assigned_agent_id IS NULL

3. Worker spawned via powershell.exe (Windows)
   └── C:\Users\goldf\rasa\.venv\Scripts\python.exe -m rasa.agent.dispatcher --soul coder-v2-dev --task-id <uuid> --one-shot

4. Dispatcher (Windows) executes
   a. Reads souls/coder-v2-dev.yaml
   b. Renders prompt with Jinja2 (Handlebars -> Jinja2)
   c. Calls LLM Gateway -> Ollama (gemma4:31b-cloud)
   d. UPDATE tasks SET status='COMPLETED', result='{...}' WHERE id=<uuid>

5. Pool Controller sees update
   └── No further action needed (task complete)

6. Hermes checks DB for done tasks
   └── SELECT id, status, result FROM tasks WHERE status='COMPLETED'
   └── Reviews result, decides next task or marks done
```

### 4.1 Failure Paths
| Scenario | Handling |
|----------|----------|
| Worker crashes before DB write | Pool controller heartbeat timeout (`> 15s`); task stays `ASSIGNED`, retry scheduled |
| LLM timeout | Gateway retries 3× with exponential backoff; final status `FAILED` |
| Policy rule violation | Policy engine writes `audit_log` + `human_reviews` row; blocks commit |
| PostgreSQL unreachable | Dispatcher exits with error code 255; Pool controller logs `backpressure_events` |

---

## 5. Files You Should Know

| File | Side | Purpose |
|------|------|---------|
| `.hermes/SOUL.md` | WSL | Hermes orchestrator context (you) |
| `.hermes/AGENTS.md` | WSL | Full system state + decisions |
| `rasa/agent/dispatcher.py` | Windows | Worker: soul -> prompt -> LLM -> DB |
| `rasa/pool/controller.py` | WSL | Polls tasks, spawns Windows workers |
| `rasa/llm_gateway/client.py` | Windows | Async Ollama client with tier routing |
| `rasa/db/conn.py` | Both | psycopg connection pool |
| `souls/*.yaml` | Both | Agent personality + model config |
| `config/gateway.yaml` | Both | LLM provider + tier mapping |
| `config/pool.yaml` | WSL | Agent sizing + replica counts |
| `migrations/010_*.sql` | DB | `rasa_orch` schema (tasks, deps, checkpoints) |
| `migrations/020_*.sql` | DB | `rasa_pool` schema (agents, heartbeats, backpressure) |
| `migrations/030_*.sql` | DB | `rasa_policy` schema (rules, audit, human reviews) |
| `migrations/040_*.sql` | DB | `rasa_memory` schema (canonical nodes, embeddings, soul sheets) |

---

## 6. Next Steps

1. **Policy Engine** (`rasa/policy/engine.py`) — evaluate `policy_rules` at task creation time, write `audit_log`
2. **Memory Subsystem** (`rasa/memory/`) — embed canonical nodes into `rasa_memory.embeddings`, vector search
3. **Sandbox Pipeline** (`rasa/sandbox/`) — semgrep + pytest after coder output
4. **Recovery Controller** (`cmd/recovery-controller/main.go`) — retry failed tasks, replay checkpoints
5. **Evaluation Engine** (`cmd/eval-aggregator/main.go`) — benchmark runs, score tasks
