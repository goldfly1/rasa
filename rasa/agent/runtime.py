"""Agent Runtime — stateful daemon that polls for tasks, calls LLM, and writes results.

Replaces the dispatcher's one-shot pipeline with a proper state machine.
Procfile agents use this module instead of dispatcher.py.

Usage:
  python -m rasa.agent.runtime --soul souls/coder-v2-dev.yaml
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import signal
import time
import uuid
from enum import Enum
from pathlib import Path
from typing import Any

import chevron
import httpx
import psycopg
import yaml

from rasa.bus.envelope import Envelope, Metadata
from rasa.bus.redis import RedisPublisher
from rasa.llm_gateway.client import GatewayClient, GatewayError

SOULS_DIR = Path(__file__).parent.parent.parent / "souls"


class AgentState(Enum):
    IDLE = "IDLE"
    WARMING = "WARMING"
    ACTIVE = "ACTIVE"
    CHECKPOINTED = "CHECKPOINTED"


def _load_soul(path: str) -> dict:
    """Load a soul sheet from a YAML file path or a soul_id name."""
    p = Path(path)
    if not p.exists():
        # Try souls/ directory by soul_id
        p = SOULS_DIR / f"{path}.yaml"
    if not p.exists():
        p = SOULS_DIR / path
    if p.exists():
        return yaml.safe_load(p.read_text())
    raise FileNotFoundError(f"Soul not found: {path}")


def _make_agent_id(soul_id: str) -> str:
    return f"agent-{soul_id}-{uuid.uuid4().hex[:8]}"


def _pg_dsn(dbname: str) -> str:
    host = os.environ.get("RASA_DB_HOST", "localhost")
    port = os.environ.get("RASA_DB_PORT", "5432")
    user = os.environ.get("RASA_DB_USER", "postgres")
    password = os.environ.get("RASA_DB_PASSWORD", "")
    return f"host={host} port={port} user={user} password={password} dbname={dbname}"


class AgentRuntime:
    """Stateful agent daemon: polls tasks, assembles prompts, calls LLM, writes results."""

    def __init__(self, soul_path: str, agent_id: str | None = None) -> None:
        self.soul = _load_soul(soul_path)
        self.agent_id = agent_id or _make_agent_id(self.soul["soul_id"])
        self.state = AgentState.IDLE
        self._running = False
        self.gateway: GatewayClient | None = None
        self.redis_pub: RedisPublisher | None = None
        self._current_task_id: str | None = None

    async def start(self) -> None:
        self._running = True
        self.gateway = GatewayClient()
        self.redis_pub = RedisPublisher()
        await self.redis_pub.connect()

        async with asyncio.TaskGroup() as tg:
            tg.create_task(self._heartbeat_loop())
            tg.create_task(self._task_poll_loop())

    async def _heartbeat_loop(self) -> None:
        session = self.soul.get("behavior", {}).get("session", {})
        interval = session.get("heartbeat_interval_seconds", 5)
        while self._running:
            meta = Metadata(
                soul_id=self.soul["soul_id"],
                agent_id=self.agent_id,
                timestamp_ms=int(time.time() * 1000),
            )
            env = Envelope.new(
                source="agent-runtime",
                destination="pool-controller",
                payload={
                    "current_state": self.state.value,
                    "soul_id": self.soul["soul_id"],
                },
                metadata=meta,
            )
            try:
                await self.redis_pub.publish(f"agents.heartbeat.{self.agent_id}", env)
            except Exception:
                pass  # heartbeat is best-effort
            await asyncio.sleep(interval)

    async def _task_poll_loop(self) -> None:
        while self._running:
            task = await self._poll_for_task()
            if task is not None:
                await self._execute_task(task)
            await asyncio.sleep(5)

    async def _poll_for_task(self) -> dict | None:
        try:
            async with await psycopg.AsyncConnection.connect(_pg_dsn("rasa_orch")) as conn:
                async with conn.transaction():
                    cur = await conn.execute(
                        "SELECT id, title, description, payload FROM tasks "
                        "WHERE assigned_agent_id = %s AND status = 'ASSIGNED' "
                        "ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED",
                        (self.agent_id,),
                    )
                    row = await cur.fetchone()
                    if row is None:
                        return None
                    task_id = str(row[0])
                    await conn.execute(
                        "UPDATE tasks SET status = 'RUNNING', started_at = NOW() WHERE id = %s",
                        (task_id,),
                    )
                    payload = row[3]
                    if isinstance(payload, str):
                        payload = json.loads(payload)
                    return {
                        "id": task_id,
                        "title": row[1],
                        "description": row[2] or "",
                        "payload": payload or {},
                    }
        except Exception:
            return None

    async def _execute_task(self, task: dict) -> None:
        self.state = AgentState.WARMING
        self._current_task_id = task["id"]

        memory = await self._assemble_memory(task)
        system_prompt = self._render_prompt(task, memory)

        self.state = AgentState.ACTIVE
        model_cfg = self.soul.get("model", {})
        try:
            result = await self.gateway.complete(
                system_prompt,
                tier=model_cfg.get("default_tier", "standard"),
                temperature=model_cfg.get("temperature", 0.2),
                max_tokens=model_cfg.get("max_tokens", 8192),
                top_p=model_cfg.get("top_p", 1.0),
            )
        except GatewayError as exc:
            await self._write_failure(task, str(exc))
            self.state = AgentState.IDLE
            self._current_task_id = None
            return

        await self._write_result(task, result)
        self.state = AgentState.IDLE
        self._current_task_id = None

    async def _assemble_memory(self, task: dict) -> dict:
        try:
            async with httpx.AsyncClient(timeout=httpx.Timeout(5)) as client:
                resp = await client.post(
                    "http://127.0.0.1:8300/assemble",
                    json={
                        "soul_id": self.soul["soul_id"],
                        "task_id": task["id"],
                        "agent_id": self.agent_id,
                        "variables": [
                            "short_term_summary",
                            "graph_excerpt",
                            "semantic_matches",
                        ],
                        "resolution": {},
                    },
                )
                resp.raise_for_status()
                return resp.json().get("variables", {})
        except Exception:
            return {
                "short_term_summary": "",
                "graph_excerpt": "",
                "semantic_matches": [],
            }

    def _render_prompt(self, task: dict, memory: dict) -> str:
        ctx = {
            "metadata": self.soul.get("metadata", {}),
            "agent_role": self.soul.get("agent_role", ""),
            "model": self.soul.get("model", {}),
            "behavior": self.soul.get("behavior", {}),
            "tools": {"enabled": []},
            "task": {
                "id": task["id"],
                "title": task["title"],
                "type": task.get("payload", {}).get("type", "generic"),
                "description": task.get("description", ""),
            },
            "memory": memory,
        }
        system = chevron.render(self.soul["prompt"]["system_template"], ctx)
        if "context_injection" in self.soul["prompt"]:
            system += "\n\n" + chevron.render(
                self.soul["prompt"]["context_injection"], ctx
            )
        return system.strip()

    async def _write_result(self, task: dict, result: dict) -> None:
        async with await psycopg.AsyncConnection.connect(_pg_dsn("rasa_orch")) as conn:
            await conn.execute(
                "UPDATE tasks SET status = 'COMPLETED', completed_at = NOW(), result = %s WHERE id = %s",
                (json.dumps(result), task["id"]),
            )
            await conn.execute("NOTIFY task_completed, %s", (task["id"],))

    async def _write_failure(self, task: dict, error_msg: str) -> None:
        async with await psycopg.AsyncConnection.connect(_pg_dsn("rasa_orch")) as conn:
            await conn.execute(
                "UPDATE tasks SET status = 'FAILED', failed_at = NOW(), error_message = %s WHERE id = %s",
                (error_msg, task["id"]),
            )
            await conn.execute("NOTIFY task_completed, %s", (task["id"],))

    async def shutdown(self) -> None:
        self._running = False
        if self.gateway:
            await self.gateway.close()
        if self.redis_pub:
            await self.redis_pub.close()


def main() -> None:
    parser = argparse.ArgumentParser(description="RASA Agent Runtime")
    parser.add_argument("--soul", required=True, help="Path to soul YAML file or soul_id")
    parser.add_argument("--agent-id", default=None, help="Override agent UUID")
    args = parser.parse_args()

    runtime = AgentRuntime(soul_path=args.soul, agent_id=args.agent_id)
    print(f"Agent {runtime.agent_id} (soul={runtime.soul['soul_id']}) starting")

    loop = asyncio.new_event_loop()
    shutdown_flag = False

    def _on_signal():
        nonlocal shutdown_flag
        if not shutdown_flag:
            shutdown_flag = True
            asyncio.ensure_future(runtime.shutdown(), loop=loop)

    try:
        loop.add_signal_handler(signal.SIGINT, _on_signal)
        loop.add_signal_handler(signal.SIGTERM, _on_signal)
    except NotImplementedError:
        pass  # Windows doesn't support add_signal_handler for SIGTERM

    try:
        loop.run_until_complete(runtime.start())
    except asyncio.CancelledError:
        pass
    except KeyboardInterrupt:
        pass
    finally:
        try:
            loop.run_until_complete(runtime.shutdown())
        except Exception:
            pass
        loop.close()
        print(f"Agent {runtime.agent_id} stopped")


if __name__ == "__main__":
    main()
