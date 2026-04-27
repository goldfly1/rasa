"""PostgreSQL connection pool shared across RASA Python components.

Reads from standard environment variables:
  RASA_DB_HOST      (default: localhost)
  RASA_DB_PORT      (default: 5432)
  RASA_DB_USER      (default: postgres)
  RASA_DB_PASSWORD  (default: empty)
  RASA_DB_NAME      (default: rasa_orch)
"""

import os
import psycopg
from psycopg_pool import ConnectionPool

_pool: ConnectionPool | None = None


def _dsn(name: str | None = None) -> str:
    host = os.environ.get("RASA_DB_HOST", "localhost")
    port = os.environ.get("RASA_DB_PORT", "5432")
    user = os.environ.get("RASA_DB_USER", "postgres")
    password = os.environ.get("RASA_DB_PASSWORD", "")
    dbname = name or os.environ.get("RASA_DB_NAME", "rasa_orch")
    return f"host={host} port={port} user={user} password={password} dbname={dbname} sslmode=disable"


def get_pool(dbname: str | None = None, min_size: int = 2, max_size: int = 10) -> ConnectionPool:
    """Return a thread-safe connection pool. Lazily initialised on first call."""
    global _pool
    if _pool is None:
        _pool = ConnectionPool(
            conninfo=_dsn(dbname),
            min_size=min_size,
            max_size=max_size,
            open=True,
        )
    return _pool


def close_pool() -> None:
    """Close the global pool if open."""
    global _pool
    if _pool is not None:
        _pool.close()
        _pool = None


def with_conn(dbname: str | None = None):
    """Decorator / context-manager helper for ad-hoc connections."""
    return psycopg.connect(_dsn(dbname))
