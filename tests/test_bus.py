"""Tests for the shared messaging layer (rasa.bus)."""
import json
import os
import pytest

from rasa.bus import Envelope, Metadata
from rasa.bus.pg import _VALID_CHANNEL


class TestEnvelope:
    def test_new_creates_ids(self):
        e = Envelope.new("orch", "pool", {"task": "build"})
        assert len(e.message_id) == 36  # UUID format
        assert len(e.correlation_id) == 36
        assert e.source_component == "orch"
        assert e.destination_component == "pool"
        assert e.payload == {"task": "build"}
        assert e.metadata.timestamp_ms > 0

    def test_new_preserves_correlation_id(self):
        e = Envelope.new("a", "b", correlation_id="abc-123")
        assert e.correlation_id == "abc-123"

    def test_new_accepts_metadata(self):
        meta = Metadata(soul_id="coder-v2-dev", agent_role="CODER", task_id="t1")
        e = Envelope.new("a", "b", {"key": "val"}, metadata=meta)
        assert e.metadata.soul_id == "coder-v2-dev"
        assert e.metadata.agent_role == "CODER"
        assert e.metadata.task_id == "t1"

    def test_to_json_produces_valid_json(self):
        meta = Metadata(soul_id="s1", agent_role="CODER", timestamp_ms=12345)
        e = Envelope.new("src", "dst", {"x": 1}, metadata=meta)
        raw = e.to_json()
        d = json.loads(raw)
        assert d["source_component"] == "src"
        assert d["metadata"]["soul_id"] == "s1"
        assert d["metadata"]["timestamp_ms"] == 12345

    def test_from_json_round_trip(self):
        meta = Metadata(soul_id="s1", prompt_version_hash="abc", agent_role="REVIEWER")
        e1 = Envelope.new("orch", "sandbox", {"files": ["a.py"]}, metadata=meta)
        e2 = Envelope.from_json(e1.to_json())
        assert e2.message_id == e1.message_id
        assert e2.correlation_id == e1.correlation_id
        assert e2.metadata.soul_id == e1.metadata.soul_id
        assert e2.metadata.agent_role == "REVIEWER"
        assert e2.payload == {"files": ["a.py"]}

    def test_from_json_bytes(self):
        e1 = Envelope.new("a", "b")
        e2 = Envelope.from_json(e1.to_json().encode())
        assert e2.message_id == e1.message_id


class TestMetadata:
    def test_defaults(self):
        m = Metadata()
        assert m.soul_id == ""
        assert m.timestamp_ms == 0

    def test_from_dict_partial(self):
        m = Metadata.from_dict({"soul_id": "test"})
        assert m.soul_id == "test"
        assert m.agent_role == ""

    def test_to_dict_excludes_none(self):
        m = Metadata(soul_id="s1", timestamp_ms=999)
        d = m.to_dict()
        assert d["soul_id"] == "s1"
        assert d["timestamp_ms"] == 999
        assert "prompt_version_hash" in d


class TestChannelValidation:
    def test_valid_channels(self):
        assert _VALID_CHANNEL.match("tasks_assigned")
        assert _VALID_CHANNEL.match("checkpoint_saved")
        assert _VALID_CHANNEL.match("a")
        assert _VALID_CHANNEL.match("A1_b2")

    def test_invalid_channels(self):
        assert not _VALID_CHANNEL.match("1bad")
        assert not _VALID_CHANNEL.match("has-dash")
        assert not _VALID_CHANNEL.match("has.dot")
        assert not _VALID_CHANNEL.match("")


class TestPostgresTransport:
    """Requires PostgreSQL running on localhost with RASA_DB_* env vars set."""

    @pytest.fixture
    def pg_dsn(self):
        pw = os.environ.get("RASA_DB_PASSWORD", "")
        if not pw:
            pytest.skip("RASA_DB_PASSWORD not set")
        return (
            f"host={os.environ.get('RASA_DB_HOST', 'localhost')} "
            f"port={os.environ.get('RASA_DB_PORT', '5432')} "
            f"user={os.environ.get('RASA_DB_USER', 'postgres')} "
            f"password={pw} "
            f"dbname=rasa_orch "
            f"sslmode=disable"
        )

    async def test_setup_creates_table(self, pg_dsn):
        from rasa.bus.pg import PostgresPublisher
        pub = PostgresPublisher(dbname="rasa_orch")
        await pub.setup()
        # Table should exist now; verify by publishing
        msg = Envelope.new("test", "pg", {"hello": "world"})
        await pub.publish("tasks_assigned", msg)

    async def test_pub_sub_round_trip(self, pg_dsn):
        import asyncio
        from rasa.bus.pg import PostgresPublisher, PostgresSubscriber

        received: list[Envelope] = []

        async def handler(env: Envelope):
            received.append(env)

        sub = PostgresSubscriber(dbname="rasa_orch")
        await sub.setup()
        await sub.subscribe("tasks_assigned", handler)
        await sub.listen("tasks_assigned")

        pub = PostgresPublisher(dbname="rasa_orch")
        await pub.setup()
        meta = Metadata(soul_id="s1", task_id="t1")
        msg = Envelope.new("orch", "pool", {"cmd": "build"}, metadata=meta)
        await pub.publish("tasks_assigned", msg)

        await asyncio.sleep(0.5)
        await sub.close()

        assert len(received) >= 1
        assert received[0].metadata.soul_id == "s1"


class TestRedisTransport:
    """Requires Redis running on localhost:6379."""

    @pytest.mark.asyncio
    async def test_pub_sub_round_trip(self):
        import asyncio
        import redis.asyncio as aioredis

        # Skip if Redis is not running
        try:
            r = await aioredis.from_url("redis://localhost:6379")
            await r.ping()
            await r.aclose()
        except Exception:
            pytest.skip("Redis not running on localhost:6379")

        from rasa.bus.redis import RedisPublisher, RedisSubscriber

        received: list[Envelope] = []

        async def handler(env: Envelope):
            received.append(env)

        from rasa.bus.redis import RedisPublisher, RedisSubscriber

        sub = RedisSubscriber(url="redis://localhost:6379")
        await sub.subscribe("test_channel", handler)
        await sub.listen()
        await asyncio.sleep(0.1)  # let subscription propagate

        pub = RedisPublisher(url="redis://localhost:6379")
        await pub.connect()
        msg = Envelope.new("a", "b", {"ping": True})
        await pub.publish("test_channel", msg)

        await asyncio.sleep(0.5)
        await sub.close()
        await pub.close()

        assert len(received) >= 1
        assert received[0].payload == {"ping": True}
