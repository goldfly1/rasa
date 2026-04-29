"""One-shot task scorer: fetches a completed task and assigns a quality score.

Usage: python -m rasa.eval.scorer --task-id <uuid>
"""

from __future__ import annotations

import argparse
import json
import os
import sys

import psycopg


def _pg_dsn(dbname: str) -> str:
    host = os.environ.get("RASA_DB_HOST", "localhost")
    port = os.environ.get("RASA_DB_PORT", "5432")
    user = os.environ.get("RASA_DB_USER", "postgres")
    password = os.environ.get("RASA_DB_PASSWORD", "")
    return f"host={host} port={port} user={user} password={password} dbname={dbname}"


def score_result(payload: dict) -> float:
    """Score a task result dict 0–1 based on structural heuristics."""
    score = 0.0

    # Has content field with actual text?
    content = payload.get("content", "")
    if content and len(content.strip()) > 10:
        score += 0.3

    # Has model field?
    if payload.get("model"):
        score += 0.1

    # Has usage info?
    usage = payload.get("usage", {})
    if usage and usage.get("completion_tokens", 0) > 0:
        score += 0.2

    # Content is well-structured (no empty/incomplete markdown)?
    if content:
        trimmed = content.strip()
        if not trimmed.endswith("..."):
            score += 0.2
        if not trimmed.startswith("Error") and not trimmed.startswith("```error"):
            score += 0.2

    return min(score, 1.0)


def main() -> None:
    parser = argparse.ArgumentParser(description="RASA Eval Scorer")
    parser.add_argument("--task-id", required=True, help="Task UUID to score")
    parser.add_argument("--dbname", default="rasa_orch", help="Database name")
    args = parser.parse_args()

    try:
        with psycopg.connect(_pg_dsn(args.dbname)) as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "SELECT result, soul_id, status FROM tasks WHERE id = %s",
                    (args.task_id,),
                )
                row = cur.fetchone()
                if row is None:
                    print(f"Task {args.task_id} not found")
                    sys.exit(1)

                result_raw = row[0]
                soul_id = row[1]
                status = row[2]

                if result_raw is None:
                    print(f"Task {args.task_id} has no result (status={status})")
                    score = 0.0 if status == "FAILED" else 0.5
                else:
                    payload = result_raw if isinstance(result_raw, dict) else json.loads(result_raw)
                    score = score_result(payload)

                output = {
                    "task_id": args.task_id,
                    "soul_id": soul_id,
                    "status": status,
                    "score": round(score, 4),
                }
                print(json.dumps(output, indent=2))

    except Exception as exc:
        print(f"Error: {exc}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
