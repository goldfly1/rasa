"""Live observability dashboard for RASA pilot.

Queries metrics views across all RASA databases and prints a human-readable
dashboard to stdout. No web UI — runs in a terminal alongside honcho.

Usage:
  python scripts/observe.py              # Refresh every 30s
  python scripts/observe.py --once       # Single snapshot, then exit
  python scripts/observe.py --interval 15
"""

from __future__ import annotations

import argparse
import os
import sys
import time
from datetime import datetime, timezone

import psycopg

SEP = "=" * 72
SUB = "-" * 48

DATABASES = [
    "rasa_orch",
    "rasa_pool",
    "rasa_eval",
    "rasa_policy",
    "rasa_recovery",
]


def _pg_dsn(dbname: str) -> str:
    host = os.environ.get("RASA_DB_HOST", "localhost")
    port = os.environ.get("RASA_DB_PORT", "5432")
    user = os.environ.get("RASA_DB_USER", "postgres")
    password = os.environ.get("RASA_DB_PASSWORD", "")
    return f"host={host} port={port} user={user} password={password} dbname={dbname}"


def query_all(dsn: str, query: str, params: tuple = ()) -> list:
    """Run a query and return all rows as list of tuples."""
    try:
        with psycopg.connect(dsn) as conn:
            with conn.cursor() as cur:
                cur.execute(query, params)
                return cur.fetchall()
    except Exception as exc:
        return [("error", str(exc))]


def query_dicts(dsn: str, query: str, params: tuple = ()) -> list[dict]:
    """Run a query and return rows as list of dicts."""
    try:
        with psycopg.connect(dsn) as conn:
            with conn.cursor() as cur:
                cur.execute(query, params)
                cols = [desc[0] for desc in cur.description] if cur.description else []
                rows = cur.fetchall()
                return [dict(zip(cols, row)) for row in rows]
    except Exception as exc:
        return [{"error": str(exc)}]


def render_dashboard() -> None:
    """Query all databases and print the dashboard."""
    now = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
    print(f"\n{SEP}")
    print(f"  RASA OBSERVABILITY DASHBOARD  |  {now}")
    print(SEP)

    # ── Tasks today (rasa_orch) ──────────────────────────────────────────
    print("\n  TASKS (last 24h)")
    print(SUB)

    orch_dsn = _pg_dsn("rasa_orch")
    rows = query_all(
        orch_dsn,
        """SELECT status, COUNT(*) FROM tasks
           WHERE created_at > NOW() - INTERVAL '24 hours'
           GROUP BY status ORDER BY status""",
    )
    status_counts = dict(rows) if rows and rows[0][0] != "error" else {}
    states = ["COMPLETED", "FAILED", "RUNNING", "ASSIGNED", "PENDING"]
    for s in states:
        count = status_counts.get(s, 0)
        bar = "#" * min(count, 50) if count else "-"
        print(f"  {s:<14} {count:>5d}  {bar}")

    total = sum(status_counts.values())
    completed = status_counts.get("COMPLETED", 0)
    failed = status_counts.get("FAILED", 0)
    success_rate = (completed / (completed + failed) * 100) if (completed + failed) > 0 else 0
    print(f"\n  Total: {total}  |  Success rate: {success_rate:.1f}%")

    # ── Agent states (rasa_pool) ─────────────────────────────────────────
    print(f"\n  AGENTS")
    print(SUB)

    pool_dsn = _pg_dsn("rasa_pool")
    agents = query_dicts(
        pool_dsn,
        """SELECT agent_id, soul_id, state,
                  EXTRACT(epoch FROM (NOW() - last_heartbeat))::INT AS idle_s
           FROM agents WHERE state != 'DISCONNECTED'
           ORDER BY state, soul_id""",
    )
    if agents and "error" not in agents[0]:
        for a in agents:
            idle_str = f"{a['idle_s']}s ago" if a["idle_s"] else "just now"
            print(f"  {a['agent_id'][:30]:<30} {a['soul_id']:<20} {a['state']:<14} {idle_str}")
    else:
        print("  (no agents registered)")

    # Uptime view
    uptime = query_dicts(pool_dsn, "SELECT * FROM v_agent_uptime")
    if uptime and "error" not in uptime[0]:
        print(f"\n  Heartbeat coverage (24h):")
        for u in uptime:
            print(
                f"  {u['agent_id'][:30]:<30} "
                f"beats={u['heartbeat_count']:<5} "
                f"span={u['span_seconds']:.0f}s "
                f"[{u['liveness']}]"
            )

    # Backpressure events
    bp = query_dicts(pool_dsn, "SELECT * FROM v_recent_backpressure")
    if bp and "error" not in bp[0]:
        print(f"\n  Backpressure events (1h): {len(bp)}")
        for b in bp[:5]:
            print(f"  {b['triggered_at']}  {b['reason']}  busy={b['agents_busy']} idle={b['agents_idle']}")

    # ── Soul performance (rasa_eval) ─────────────────────────────────────
    print(f"\n  SOUL PERFORMANCE (7d)")
    print(SUB)

    eval_dsn = _pg_dsn("rasa_eval")
    perf = query_dicts(eval_dsn, "SELECT * FROM v_soul_performance")
    if perf and "error" not in perf[0]:
        print(f"  {'Soul':<20} {'Tasks':>6} {'Avg':>7} {'Pass%':>7} {'AvgMs':>7} {'Low':>5}")
        print(f"  {'-'*20} {'-'*6} {'-'*7} {'-'*7} {'-'*7} {'-'*5}")
        for p in perf:
            print(
                f"  {p['soul_id']:<20} "
                f"{p['task_count']:>6} "
                f"{p['avg_score']:>7.3f} "
                f"{p['pass_rate']:>6.1%} "
                f"{p['avg_duration_ms']:>7.0f} "
                f"{p['low_score_count']:>5}"
            )
    else:
        print("  (no evaluation data)")

    # Drift snapshots
    drift = query_dicts(eval_dsn, "SELECT * FROM v_latest_drift")
    if drift and "error" not in drift[0]:
        print(f"\n  Drift status:")
        for d in drift:
            flag = "⚠  DRIFT" if d["flagged"] else "OK"
            print(
                f"  {d['soul_id']:<20} n={d['window_size']} "
                f"mean={d['mean_score']:.3f} std={d['std_score']:.3f} [{flag}]"
            )

    # ── Recovery actions (rasa_recovery) ─────────────────────────────────
    rec_dsn = _pg_dsn("rasa_recovery")
    rec = query_dicts(rec_dsn, "SELECT * FROM v_recent_recoveries")
    if rec and "error" not in rec[0]:
        print(f"\n  RECOVERY ACTIONS (24h)")
        print(SUB)
        for r in rec:
            print(f"  {r['action']:<20} count={r['count']:<4} hour={r['hour']}")

    # ── Policy decisions (rasa_policy) ───────────────────────────────────
    pol_dsn = _pg_dsn("rasa_policy")
    dec = query_dicts(pol_dsn, "SELECT * FROM v_recent_decisions")
    if dec and "error" not in dec[0]:
        print(f"\n  POLICY DECISIONS (24h)")
        print(SUB)
        for d in dec:
            print(f"  {d['decision']:<14} count={d['count']:<4} hour={d['hour']}")

    print(f"\n{SEP}\n")


def main() -> None:
    parser = argparse.ArgumentParser(description="RASA Observability Dashboard")
    parser.add_argument(
        "--interval", type=int, default=30,
        help="Refresh interval in seconds (default: 30)",
    )
    parser.add_argument(
        "--once", action="store_true",
        help="Print one snapshot and exit",
    )
    args = parser.parse_args()

    if args.once:
        render_dashboard()
    else:
        print("RASA Observability Dashboard (Ctrl-C to exit)")
        try:
            while True:
                render_dashboard()
                time.sleep(args.interval)
        except KeyboardInterrupt:
            print("\nExiting.")


if __name__ == "__main__":
    main()
