"""Windows-side agent dispatcher — called from WSL via powershell.exe or run directly.

Usage (from WSL):
  powershell.exe -Command "C:\Users\goldf\rasa\.venv\Scripts\python.exe -m rasa.agent.dispatcher --soul coder-v2-dev --task-id <uuid> --one-shot"

Or from Windows:
  python -m rasa.agent.dispatcher --soul planner-v1 --goal "Design caching module"
"""

from __future__ import annotations

import asyncio
import json
import os
import sys
import argparse
import time
import signal
from pathlib import Path
from typing import Any

import yaml
import httpx
import psycopg


SOULS_DIR = Path(__file__).parent.parent.parent / "souls"
CHECKPOINTS_DIR = Path("C:/Users/goldf/rasa/checkpoints")
CHECKPOINTS_DIR.mkdir(parents=True, exist_ok=True)


def _pg_conn(dbname: str = "rasa_orch"):
    pw = os.environ.get("RASA_DB_PASSWORD", "")
    return psycopg.connect(
        host=os.environ.get("RASA_DB_HOST", "localhost"),
        port=int(os.environ.get("RASA_DB_PORT", "5432")),
        user=os.environ.get("RASA_DB_USER", "postgres"),
        password=pw,
        dbname=dbname,
        sslmode="disable",
    )


def _load_soul(soul_id: str) -> dict[str, Any]:
    for p in SOULS_DIR.glob("*.yaml"):
        with open(p) as f:
            doc = yaml.safe_load(f)
            if doc.get("soul_id") == soul_id:
                return doc
    raise FileNotFoundError(f"Soul {soul_id} not found in {SOULS_DIR}")


def _resolve_model(soul: dict[str, Any], override: str | None) -> tuple[str, str]:
    """Return (base_url, model_name)."""
    default = os.environ.get("RASA_DEFAULT_MODEL", "gemma4:31b-cloud")
    if override:
        model = override
    elif soul.get("model", {}).get("preferred_model"):
        model = soul["model"]["preferred_model"]
    else:
        tier = soul.get("model", {}).get("default_tier", "standard")
        if tier == "premium":
            model = os.environ.get("RASA_PREMIUM_MODEL", "kimi-k2.6:cloud")
        else:
            model = default
    base_url = os.environ.get("OLLAMA_BASE_URL", "http://127.0.0.1:11434/v1")
    return base_url, model


def _render_system_prompt(soul: dict[str, Any], task: dict[str, Any], memory: dict[str, Any]) -> str:
    """Fill the soul's system_template with task and memory context."""
    import chevron

    ctx = {
        "metadata": soul["metadata"],
        "agent_role": soul["agent_role"],
        "task": task,
        "memory": memory,
        "tools": {"enabled": []},
    }
    template = soul["prompt"]["system_template"]
    body = chevron.render(template, ctx)

    # Append context injection if present
    if "context_injection" in soul["prompt"]:
        body += "\n\n" + chevron.render(soul["prompt"]["context_injection"], ctx)

    return body.strip()


async def _call_llm(base_url: str, model: str, messages: list[dict], temperature: float, max_tokens: int) -> dict[str, Any]:
    api_key = os.environ.get("OLLAMA_API_KEY", "ollama")
    payload = {
        "model": model,
        "messages": messages,
        "stream": False,
        "temperature": temperature,
        "max_tokens": max_tokens,
    }
    async with httpx.AsyncClient(timeout=httpx.Timeout(120)) as c:
        r = await c.post(
            f"{base_url}/chat/completions",
            headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"},
            json=payload,
        )
        r.raise_for_status()
        data = r.json()
        return {
            "content": data["choices"][0]["message"]["content"],
            "model": data["model"],
            "usage": data.get("usage", {}),
        }


async def run_task(soul_id: str, task_id: str | None, goal: str | None, model_override: str | None, dry_run: bool, one_shot: bool) -> dict[str, Any]:
    soul = _load_soul(soul_id)
    base_url, model = _resolve_model(soul, model_override)

    # Fetch or create task row
    with _pg_conn("rasa_orch") as conn:
        with conn.cursor() as cur:
            if task_id:
                cur.execute("SELECT id, title, description, payload, status FROM tasks WHERE id = %s", (task_id,))
                row = cur.fetchone()
                if not row:
                    raise ValueError(f"Task {task_id} not found")
                task = {"id": str(row[0]), "title": row[1], "description": row[2] or "", "type": (row[3] or {}).get("type", "generic"), "payload": row[3] or {}}
                cur.execute("UPDATE tasks SET status = 'RUNNING', started_at = NOW() WHERE id = %s", (task_id,))
            else:
                # Ad-hoc one-shot: create orphaned task row
                cur.execute(
                    "INSERT INTO tasks (title, description, payload, status, soul_id) VALUES (%s, %s, %s, 'RUNNING', %s) RETURNING id",
                    (goal or f"Ad-hoc {soul_id}", "", json.dumps({"type": "ad-hoc", "goal": goal}), soul_id),
                )
                tid = str(cur.fetchone()[0])
                task = {"id": tid, "title": goal or f"Ad-hoc {soul_id}", "description": "", "type": "ad-hoc", "payload": {"goal": goal}}
                task_id = tid
            conn.commit()

    # Build prompt
    memory = {"short_term_summary": "", "graph_excerpt": "", "diff_summary": "", "semantic_matches": "[]"}
    system_prompt = _render_system_prompt(soul, task, memory)
    messages = [{"role": "system", "content": system_prompt}]
    if task.get("description"):
        messages.append({"role": "user", "content": task["description"]})
    elif goal:
        messages.append({"role": "user", "content": goal})

    temperature = soul.get("model", {}).get("temperature", 0.2)
    max_tokens = soul.get("model", {}).get("max_tokens", 4096)

    if dry_run:
        result = {"dry_run": True, "messages": messages, "model": model}
    else:
        result = await _call_llm(base_url, model, messages, temperature, max_tokens)

    # Store result
    if not dry_run:
        with _pg_conn("rasa_orch") as conn:
            with conn.cursor() as cur:
                status = "COMPLETED" if one_shot else "CHECKPOINTED"
                cur.execute(
                    "UPDATE tasks SET status = %s, completed_at = NOW(), result = %s WHERE id = %s",
                    (status, json.dumps(result), task_id),
                )
                if not one_shot:
                    # Write checkpoint file in addition to DB
                    checkpoint_path = CHECKPOINTS_DIR / f"{task_id}.json"
                    checkpoint_path.write_text(json.dumps({"task_id": task_id, "soul_id": soul_id, "turn": result}, indent=2), encoding="utf-8")
                    cur.execute(
                        "INSERT INTO checkpoint_refs (task_id, agent_id, snapshot_path, metadata) VALUES (%s, %s, %s, %s)",
                        (task_id, f"agent-{soul_id}", str(checkpoint_path), json.dumps({"turn": 1})),
                    )
                conn.commit()

    return {"task_id": task_id, "soul_id": soul_id, **result}


async def daemon_loop(soul_id: str, task_id: str, model_override: str | None, interval: int = 5):
    """Heartbeat loop for daemon-mode agents.

    Polls `tasks` table for new work matching this soul, or waits for
    human / orchestrator signals via DB row updates.
    """
    stop_requested = False

    def _handle_sig(signum, frame):
        nonlocal stop_requested
        stop_requested = True

    signal.signal(signal.SIGTERM, _handle_sig)
    signal.signal(signal.SIGINT, _handle_sig)

    print(f"[{soul_id}] daemon starting for task {task_id}")

    while not stop_requested:
        with _pg_conn("rasa_orch") as conn:
            with conn.cursor() as cur:
                # Touch heartbeat equivalent in tasks row
                cur.execute(
                    "UPDATE tasks SET started_at = COALESCE(started_at, NOW()) WHERE id = %s AND status IN ('ASSIGNED', 'RUNNING', 'CHECKPOINTED')",
                    (task_id,),
                )
                # In a real daemon we'd poll for sub-tasks or messages; for Phase 1 we just checkpoint periodically
                cur.execute("SELECT status FROM tasks WHERE id = %s", (task_id,))
                row = cur.fetchone()
                if not row or row[0] in ("COMPLETED", "FAILED", "CANCELLED"):
                    print(f"[{soul_id}] task {task_id} terminal state {row[0] if row else 'gone'}; shutting down")
                    conn.commit()
                    break
                conn.commit()

        print(f"[{soul_id}] heartbeat")
        time.sleep(interval)

    print(f"[{soul_id}] daemon stopped")


def main():
    parser = argparse.ArgumentParser(description="RASA Windows-side agent dispatcher")
    parser.add_argument("--soul", required=True, help="Soul id (e.g. coder-v2-dev)")
    parser.add_argument("--task-id", default=None, help="Existing task UUID")
    parser.add_argument("--goal", default=None, help="Ad-hoc goal text (creates task if --task-id absent)")
    parser.add_argument("--model-override", default=None, help="Force a specific LLM model")
    parser.add_argument("--dry-run", action="store_true", help="Render prompt but don't call LLM")
    parser.add_argument("--one-shot", action="store_true", default=True, dest="one_shot", help="Run once and exit (default)")
    parser.add_argument("--daemon", action="store_false", dest="one_shot", help="Run heartbeat loop (daemon mode)")
    parser.add_argument("--heartbeat-interval", type=int, default=5, help="Seconds between heartbeats")
    args = parser.parse_args()

    soul = _load_soul(args.soul)
    session_mode = "daemon" if not args.one_shot else "one-shot"
    configured_mode = soul.get("behavior", {}).get("session", {}).get("mode", "one-shot")
    if configured_mode == "daemon" and session_mode != "daemon":
        print(f"[{args.soul}] configured for daemon but --one-shot passed; forcing daemon")
        args.one_shot = False

    if args.one_shot:
        result = asyncio.run(
            run_task(
                soul_id=args.soul,
                task_id=args.task_id,
                goal=args.goal,
                model_override=args.model_override,
                dry_run=args.dry_run,
                one_shot=True,
            )
        )
        print(json.dumps(result, indent=2))
    else:
        if not args.task_id:
            # Create a persistent task row for daemon-mode
            with _pg_conn("rasa_orch") as conn:
                with conn.cursor() as cur:
                    goal = args.goal or f"Daemon {args.soul}"
                    cur.execute(
                        "INSERT INTO tasks (title, description, payload, status, soul_id) VALUES (%s, %s, %s, 'ASSIGNED', %s) RETURNING id",
                        (goal, "", json.dumps({"type": "daemon", "goal": goal}), args.soul),
                    )
                    tid = str(cur.fetchone()[0])
                    conn.commit()
                    args.task_id = tid
        # Run initial turn
        asyncio.run(
            run_task(
                soul_id=args.soul,
                task_id=args.task_id,
                goal=args.goal,
                model_override=args.model_override,
                dry_run=args.dry_run,
                one_shot=False,
            )
        )
        # Enter loop
        asyncio.run(
            daemon_loop(
                soul_id=args.soul,
                task_id=args.task_id,  # type: ignore[arg-type]
                model_override=args.model_override,
                interval=args.heartbeat_interval,
            )
        )


if __name__ == "__main__":
    main()
