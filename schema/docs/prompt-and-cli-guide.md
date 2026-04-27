# Prompt Templating & CLI Invocation Guide

> **Reference:** `../implementation/agent_configuration.md`  
> **Status:** Draft  
> **Last Updated:** 2026-04-25

---

## 1. Prompt Templating System

This is the machinery that turns a soul sheet\'s static template into the actual string sent to the LLM. Think of it as a mail-merge system: the template is the form letter, and the variables are the recipient-specific data.

### 1.1 Why Handlebars?

Handlebars is a **logic-light** templating language. It supports simple variable substitution (`{{name}}`) and loops (`{{#each items}}`), but not arbitrary code execution. This is intentional:

- **Deterministic:** Same inputs always produce the same output string ? same SHA-256 hash ? LLM Gateway cache hit.
- **Portable:** Go and Python both have mature Handlebars implementations, so the same template works across the control plane and agent runtime.
- **Safe:** No code injection risk from template expressions.

### 1.2 The 5-Layer Variable Resolution Stack

When the Agent Runtime assembles a prompt, it resolves variables from five sources, in this order (later layers override earlier ones):

| Layer | Source | Example Variable | Example Value |
|-------|--------|------------------|---------------|
| **1. Soul defaults** | The YAML soul sheet itself | `metadata.name` | `"Senior Coder"` |
| **2. Memory** | Redis / pgvector / graph store | `memory.short_term_summary` | `"Recent refactor of auth module"` |
| **3. Task envelope** | Orchestrator\'s assignment | `task.title` | `"Add OAuth2 login"` |
| **4. CLI arguments** | Command-line overrides | `--model-override` | `"gpt-5.4"` |
| **5. Environment variables** | Deployment-level config | `RASA_BUDGET_TIER` | `"standard"` |

**Precedence rule:** If the same variable is defined in multiple layers, the higher-numbered layer wins. For example, a `--model-override` CLI argument overrides the `model.preferred_model` default in the soul sheet.

### 1.3 Concrete Example

Given this soul sheet template:

```yaml
prompt:
  system_template: |
    You are {{metadata.name}}, an {{agent_role}} agent.
    Current task: {{task.title}}
    Budget tier: {{model.default_tier}}
```

And these resolved variables:

```json
{
  "metadata": { "name": "Senior Coder" },
  "agent_role": "CODER",
  "task": { "title": "Add OAuth2 login" },
  "model": { "default_tier": "standard" }
}
```

The assembled prompt becomes:

```
You are Senior Coder, an CODER agent.
Current task: Add OAuth2 login
Budget tier: standard
```

### 1.4 The Assembly Pipeline

```
?????????????????     ?????????????????     ?????????????????     ?????????????????
?  Soul Sheet   ?  ?  ?  Context      ?  ?  ?  Template     ?  ?  ?  Final Prompt ?
?  (YAML)       ?     ?  Resolver     ?     ?  Engine       ?     ?  (String)     ?
?????????????????     ?????????????????     ?????????????????     ?????????????????
                           ?
        ???????????????????????????????????????
        ? Memory ? Task ? CLI args ? Env vars ?
        ???????????????????????????????????????
```

**Step by step:**
1. Load the soul sheet YAML into memory.
2. Resolve inheritance (merge parent ? child).
3. Query Memory Subsystem for `memory.short_term_summary`, `memory.graph_excerpt`, etc.
4. Overlay task envelope fields (`task.title`, `task.type`).
5. Overlay CLI arguments (`--model-override`, `--verbose`).
6. Overlay environment variables (`RASA_BUDGET_TIER`, `RASA_AGENT_ROLE`).
7. Feed the merged context into Handlebars.
8. Render the final string.
9. Compute SHA-256 hash for cache lookup.

### 1.5 Caching Strategy

After assembly, the Agent Runtime computes a **SHA-256 hash** of:

```
hash = SHA-256(final_prompt_string + model_id + temperature + max_tokens)
```

This hash is sent to the LLM Gateway in the `ModelRequest` envelope.

**Cache hit:** Gateway returns the previously computed completion without calling the model API.
**Cache miss:** Gateway calls the model, stores the result with a TTL (default 1 hour).

**Invalidation triggers:**
- Soul sheet changes ? new `prompt_version_hash`
- TTL expiration
- Explicit admin flush command
- `souls.update` event

---

## 2. CLI Invocation Model

This defines how a human or CI system can invoke an agent directly from the command line, bypassing the Orchestrator and warm pool.

### 2.1 Why Three Session Modes?

Different use cases need different process lifecycles:

| Mode | Analogy | Use Case |
|------|---------|----------|
| **One-shot** | `curl` ? fire a request, get a response, exit | CI/CD pipelines, batch jobs, pre-commit hooks |
| **Interactive** | `python` REPL ? start, type commands, see output | Local debugging, prompt engineering, exploratory coding |
| **Daemon** | `nginx` ? start, stay running, handle requests | Production warm pool, long-running agent fleet |

### 2.2 Mode Selection Priority

The effective mode is determined by the first match in this priority order:

1. `--mode one-shot|interactive|daemon` (explicit CLI flag)
2. `--one-shot` (shorthand ? see ?2.3 below)
3. `RASA_AGENT_MODE` (environment variable)
4. Soul sheet default: `behavior.session.mode: daemon`

### 2.3 The `--one-shot` Flag

**Yes ? `--one-shot` is a shorthand for one-off use of an agent configuration.**

It is exactly equivalent to `--mode one-shot`, but more ergonomic. When specified:
- The agent loads the requested soul sheet.
- Processes exactly one task (either from `--task-id` or stdin).
- Runs through the full pipeline: prompt assembly ? LLM call ? sandbox gate ? result emission.
- **Exits immediately** after emitting the result and exit code.
- Does **not** register with the Pool Controller.
- Does **not** heartbeat.
- Does **not** checkpoint to Redis/PostgreSQL (unless `--checkpoint` is also passed).

**Typical usage:**

```bash
rasa-agent --soul souls/coder-v2-dev.yaml \
           --one-shot \
           --task-id 0195f... \
           --file src/auth.py
```

**Why this matters:** One-shot mode lets you treat an agent like a compiler or linter. You feed it a task, it produces output, and you integrate that output into your existing toolchain. It is the primary mode for:
- GitHub Actions / GitLab CI jobs
- Local pre-commit hooks
- Batch processing scripts
- Quick ad-hoc tasks without warming a daemon

### 2.4 Argument Binding

The soul sheet defines a map between CLI flags and internal soul fields:

```yaml
cli:
  argument_binding:
    --task-id: "task.id"
    --file: "task.context_files[]"
    --dry-run: "behavior.dry_run"
    --model-override: "model.preferred_model"
    --verbose: "behavior.verbose_logging"
    --one-shot: "behavior.session.mode"     # Sets mode to "one-shot"
```

**Example invocation and internal state:**

```bash
rasa-agent --soul souls/coder-v2-dev.yaml \
           --task-id 0195f... \
           --file src/auth.py \
           --dry-run \
           --verbose
```

| CLI Flag | Soul Field | Value After Binding |
|----------|-----------|---------------------|
| `--task-id` | `task.id` | `"0195f..."` |
| `--file` | `task.context_files[]` | `["src/auth.py"]` |
| `--dry-run` | `behavior.dry_run` | `true` |
| `--verbose` | `behavior.verbose_logging` | `true` |
| *(not present)* | `model.preferred_model` | `"gpt-5.4"` (soul default) |

### 2.5 Environment Injection

Environment variables are automatically mapped to soul fields at startup:

```yaml
cli:
  environment_injection:
    RASA_AGENT_ROLE: "agent_role"
    RASA_SOUL_ID: "soul_id"
    RASA_BUDGET_TIER: "model.default_tier"
```

```bash
export RASA_BUDGET_TIER="premium"
rasa-agent --soul souls/coder-v2-dev.yaml --one-shot --task-id $TASK_ID
# ? model.default_tier becomes "premium" (overriding soul sheet default)
```

### 2.6 Variable Resolution Order (Full Precedence)

When a value is defined in multiple places, this order wins:

1. **CLI arguments** (most specific)
2. **Environment variables**
3. **Task envelope** from Orchestrator
4. **Memory Subsystem** context
5. **Soul sheet defaults** (least specific)

### 2.7 Exit Codes

Every CLI agent returns a numeric exit code. The soul sheet defines the mapping:

```yaml
cli:
  exit_codes:
    success: 0
    validation_failure: 1
    sandbox_rejection: 2
    budget_exhausted: 3
    checkpoint_failure: 4
    unknown_error: 255
```

**Usage in CI/CD:**

```bash
rasa-agent --soul souls/coder-v2-dev.yaml --one-shot --task-id $TASK_ID
EXIT_CODE=$?

case $EXIT_CODE in
  0) echo "Success";;
  1) echo "Validation failed ? check task parameters";;
  2) echo "Sandbox rejected output ? security gate failed";;
  3) echo "Budget exhausted ? retry with lower tier";;
  4) echo "Checkpoint failure ? session state lost";;
  255) echo "Unknown error ? escalate to platform team";;
esac
```

### 2.8 Full Invocation Flow

```
???????????????     ???????????????????????     ???????????????????
?   User /    ?     ?   Agent Runtime       ?     ?   LLM Gateway   ?
?   CI calls  ???????   (CLI parser)        ???????   (ModelRequest)?
?   rasa-agent?     ?                       ?     ?                 ?
???????????????     ?????????????????????????     ???????????????????
                           ?
        ???????????????????????????????????????
        ?                  ?                  ?
  ????????????     ????????????     ????????????????
  ? Parse    ?     ? Bind to  ?     ? Resolve env  ?
  ? CLI args ?     ? soul     ?     ? variables    ?
  ?          ?     ? fields   ?     ?              ?
  ????????????     ????????????     ????????????????
        ?
        ?
  ???????????????????????????????????????????
  ? Load Memory context (short_term,        ?
  ? semantic matches, graph traversal)       ?
  ???????????????????????????????????????????
        ?
        ?
  ???????????????????????????????????????????
  ? Assemble prompt (Handlebars + 5-layer   ?
  ? stack) ? compute hash ? send to Gateway ?
  ???????????????????????????????????????????
        ?
        ?
  ???????????????????????????????????????????
  ? Receive completion ? run sandbox gate    ?
  ? ? emit exit code ? exit (if one-shot)   ?
  ???????????????????????????????????????????
```

---

## 3. One-Shot vs. Daemon: When to Use Which

| Scenario | Recommended Mode | Rationale |
|----------|-----------------|-----------|
| CI/CD pipeline job | `--one-shot` | Deterministic lifetime; no warm pool overhead |
| Local debugging | `interactive` | REPL-like exploration; human in the loop |
| Production task queue | `daemon` | Low latency; pre-warmed; integrates with Pool Controller |
| Batch processing 10k files | `--one-shot` with loop wrapper | Each file is an independent task; no need for session state |
| Long-running design discussion | `interactive` or `daemon` | Context accumulates across turns |
| Pre-commit hook | `--one-shot` | Fast; exits on completion; no background process |

---

## 4. One-Shot Special Behaviors

One-shot mode has three important behavioral differences from daemon mode:

| Behavior | One-Shot | Daemon |
|----------|----------|--------|
| **Heartbeat** | Disabled | Enabled per `heartbeat_interval_seconds` |
| **Pool Controller registration** | Skipped | Required |
| **Checkpointing** | Optional (`--checkpoint`) | Automatic per `checkpoint_interval_seconds` |
| **Session state cleanup** | Immediate on exit | Persists until `max_idle_minutes` timeout |
| **Tool policy gating** | Policy Engine still enforced | Policy Engine still enforced |
| **Budget tracking** | Still enforced; exits with code 3 if exceeded | Still enforced; task escalated if exceeded |

---

*This guide is derived from `agent_configuration.md`. For the authoritative schema, refer to the implementation document.*
