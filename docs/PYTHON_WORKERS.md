# RASA Python Worker Architecture

## Overview

The Python workers inside `C:\Users\goldf\rasa\.venv` are agent processes that:
1. Read YAML soul sheets defining agent personality
2. Resolve tier/model from `config/gateway.yaml`
3. Render system prompts via Jinja2 (Handlebars→Jinja2 preprocessor)
4. Call Ollama's OpenAI-compatible API via `httpx`
5. Write results back to PostgreSQL `rasa_orch.tasks`
6. Support both one-shot execution and daemon heartbeat mode

## Entry Points

### `rasa/agent/dispatcher.py`
Main worker script. Run directly on Windows.

```bash
# One-shot: run once, update task to COMPLETED
python -m rasa.agent.dispatcher --soul coder-v2-dev --goal "Refactor DB layer" --one-shot

# Daemon: run until task reaches terminal state
python -m rasa.agent.dispatcher --soul coder-v2-dev --task-id <uuid> --daemon
```

### `rasa/pool/controller.py`
Task poller and worker spawner.

```python
class PoolController:
    def run(self):
        while True:
            rows = self._poll_pending()
            for task_id, soul_id in rows:
                self._spawn_worker(task_id, soul_id)
            time.sleep(2)
```

## Soul Sheet Processing

### Step 1: Load
```python
def _load_soul(soul_id):
    for p in SOULS_DIR.glob("*.yaml"):
        with open(p) as f:
            doc = yaml.safe_load(f)
            if doc["soul_id"] == soul_id:
                return doc
```

### Step 2: Resolve model
```python
def _resolve_model(soul, override=None):
    if override: return override
    tier = soul["model"]["default_tier"]  # "standard" or "premium"
    if tier == "premium":
        return os.environ.get("RASA_PREMIUM_MODEL", "kimi-k2.6:cloud")
    return os.environ.get("RASA_DEFAULT_MODEL", "gemma4:31b-cloud")
```

### Step 3: Render prompt
Soul sheets use Handlebars syntax. The dispatcher converts `{{#each}}` to Jinja2 `{% for %}` before rendering:

```python
def hb_to_jinja(text):
    text = re.sub(r'\{\{#each\s+([\w.]+)\s*\}\}', r'{% for item in \1 %}', text)
    text = text.replace('{{this}}', '{{item}}')
    text = re.sub(r'\{\{/each\s*\}\}', '{% endfor %}', text)
    return text

env = jinja2.Environment(undefined=jinja2.StrictUndefined)
body = env.from_string(hb_to_jinja(template)).render(ctx)
```

Context dict: `{"metadata": ..., "agent_role": ..., "task": ..., "memory": ..., "tools": ...}`

### Step 4: Call LLM
```python
async def _call_llm(base_url, model, messages, temperature, max_tokens):
    payload = {"model": model, "messages": messages, "stream": False}
    async with httpx.AsyncClient(timeout=120) as c:
        r = await c.post(f"{base_url}/chat/completions", json=payload)
        return r.json()["choices"][0]["message"]["content"]
```

### Step 5: Store result
```python
with psycopg.connect(...) as conn:
    cur = conn.cursor()
    cur.execute(
        "UPDATE tasks SET status = %s, result = %s WHERE id = %s",
        ("COMPLETED", json.dumps(result), task_id)
    )
    conn.commit()
```

## LLM Gateway Client

`rasa/llm_gateway/client.py` is the async client:

```python
from rasa.llm_gateway import GatewayClient
client = GatewayClient()
result = await client.complete(
    prompt="Implement caching",
    tier="standard",  # resolves to gemma4:31b-cloud
    temperature=0.2,
)
```

Features:
- Tiered routing (standard → gemma4:31b-cloud, premium → kimi-k2.6:cloud)
- Retry with exponential backoff (3 attempts)
- Cache responses in `rasa_memory` embeddings table (best-effort)
- Fetch available models from Ollama `/api/tags`

## Database Layer

`rasa/db/conn.py` provides a lazily-initialized psycopg ConnectionPool:

```python
from rasa.db import get_pool
pool = get_pool("rasa_orch")
```

Workers use ad-hoc connections for one-shot tasks.

## Daemon Mode

Some souls are configured for daemon mode (persistent workers with heartbeats):

```yaml
behavior:
  session:
    mode: daemon
    max_idle_minutes: 10
    heartbeat_interval_seconds: 5
```

In daemon mode:
1. Task row status = `ASSIGNED`
2. Worker runs initial LLM turn
3. Enters `daemon_loop()`: every 5 seconds touches `started_at`, checks for terminal state
4. Exits if task becomes COMPLETED/FAILED/CANCELLED
5. On SIGTERM/SIGINT, graceful shutdown

## Task Row Lifecycle

```
PENDING -> ASSIGNED (pool controller claims it)
ASSIGNED -> RUNNING (worker starts)
RUNNING -> CHECKPOINTED (daemon mode, mid-work)
RUNNING -> COMPLETED (one-shot or daemon done)
RUNNING -> FAILED (exception)
ANY -> CANCELLED (human intervention)
```
