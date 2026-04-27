"""Pool controller — WSL-side process that spawns Windows workers and tracks heartbeats.

Usage:
  python -m rasa.pool start --pool-file config/pool.yaml
"""

from __future__ import annotations

import asyncio
import json
import os
import subprocess
import time
from pathlib import Path
from typing import Any

import yaml
import psycopg


CONFIG_PATH = Path(__file__).parent.parent.parent / "config" / "pool.yaml"


def _pg_conn(dbname: str):
    pw = os.environ.get("RASA_DB_PASSWORD", "")
    return psycopg.connect(
        host=os.environ.get("RASA_DB_HOST", "localhost"),
        port=int(os.environ.get("RASA_DB_PORT", "5432")),
        user=os.environ.get("RASA_DB_USER", "postgres"),
        password=pw,
        dbname=dbname,
        sslmode="disable",
    )


def _load_pool_config(path: Path | str) -> dict[str, Any]:
    with open(path) as f:
        return yaml.safe_load(f)


class PoolController:
    """Polls `tasks` for READY work, spawns Windows-side workers via powershell.exe."""

    def __init__(self, cfg: dict[str, Any]) -> None:
        self.cfg = cfg
        self.interactive = False

    def run(self) -> None:
        print("[pool] controller starting")
        while True:
            with _pg_conn("rasa_orch") as conn:
                with conn.cursor() as cur:
                    # Find tasks in PENDING with no assigned agent
                    cur.execute(
                        """
                        SELECT id, title, description, payload, soul_id, priority
                        FROM tasks
                        WHERE status = 'PENDING' AND assigned_agent_id IS NULL
                        ORDER BY priority ASC, created_at ASC
                        LIMIT 10
                        """
                    )
                    rows = cur.fetchall()
                    for row in rows:
                        task_id, title, desc, payload, soul_id, priority = row
                        self._spawn(task_id, soul_id, title)
                    conn.commit()
            time.sleep(2)

    def _spawn(self, task_id: str, soul_id: str, goal: str | None) -> None:
        """Dispatch a task to Windows side via powershell.exe."""
        venv_python = r"C:\Users\goldf\rasa\.venv\Scripts\python.exe"
        script = r"C:\Users\goldf\rasa\rasa\agent\dispatcher.py"
        cmd = [
            "powershell.exe",
            "-Command",
            f"Set-Item -Path Env:RASA_DB_PASSWORD -Value $env:RASA_DB_PASSWORD; & '{venv_python}' -m rasa.agent.dispatcher --soul {soul_id} --task-id {task_id} --one-shot",
        ]
        print(f"[pool] spawning {soul_id} for task {task_id}")
        # Fire and forget (detached by Windows shell)
        subprocess.Popen(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)


def main():
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("--pool-file", default=str(CONFIG_PATH), help="Path to pool.yaml")
    args = parser.parse_args()
    cfg = _load_pool_config(args.pool_file)
    ctrl = PoolController(cfg)
    ctrl.run()


if __name__ == "__main__":
    main()
