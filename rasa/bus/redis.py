"""Redis Pub/Sub transport — ephemeral, loss-tolerant messaging (heartbeats, policy)."""

from __future__ import annotations

import asyncio
from typing import Awaitable, Callable

import redis.asyncio as aioredis

from rasa.bus.envelope import Envelope


class RedisPublisher:
    """Publish envelope as JSON to a Redis channel."""

    def __init__(self, *, url: str = "redis://localhost:6379") -> None:
        self._url = url
        self._client: aioredis.Redis | None = None

    async def connect(self) -> None:
        self._client = await aioredis.from_url(self._url)

    async def publish(self, channel: str, msg: Envelope) -> None:
        if self._client is None:
            raise RuntimeError("RedisPublisher not connected — call await connect()")
        await self._client.publish(channel, msg.to_json())

    async def close(self) -> None:
        if self._client is not None:
            await self._client.aclose()


class RedisSubscriber:
    """Subscribe to Redis channel(s) and dispatch messages to handlers.

    Supports glob patterns via PSUBSCRIBE (e.g. 'agents.heartbeat.*').
    """

    def __init__(self, *, url: str = "redis://localhost:6379") -> None:
        self._url = url
        self._client: aioredis.Redis | None = None
        self._handlers: dict[str, Callable[[Envelope], Awaitable[None]]] = {}

    async def subscribe(self, channel_or_pattern: str, handler: Callable[[Envelope], Awaitable[None]]) -> None:
        self._handlers[channel_or_pattern] = handler

    async def listen(self) -> None:
        self._client = await aioredis.from_url(self._url)
        pubsub = self._client.pubsub()

        # Deduplicate: subscribe exact channels, psubscribe patterns
        exact: list[str] = []
        patterns: list[str] = []
        for ch in self._handlers:
            if "*" in ch or "?" in ch or "[" in ch:
                patterns.append(ch)
            else:
                exact.append(ch)

        if exact:
            await pubsub.subscribe(*exact)
        if patterns:
            await pubsub.psubscribe(*patterns)

        async def _drain() -> None:
            async for msg in pubsub.listen():
                mtype = msg["type"]
                if isinstance(mtype, bytes):
                    mtype = mtype.decode()
                if mtype not in ("message", "pmessage"):
                    continue
                data = msg.get("data", b"")
                if isinstance(data, bytes):
                    data = data.decode()
                if not data:
                    continue
                channel = msg.get("channel", b"")
                if isinstance(channel, bytes):
                    channel = channel.decode()
                pattern = msg.get("pattern")
                if pattern is not None and isinstance(pattern, bytes):
                    pattern = pattern.decode()
                matched: str | None = pattern or channel or None
                if matched is None:
                    continue
                handler = self._handlers.get(matched)
                if handler is not None:
                    try:
                        await handler(Envelope.from_json(data))
                    except Exception:
                        pass

        self._drain_task = asyncio.create_task(_drain())

    async def close(self) -> None:
        if hasattr(self, "_drain_task"):
            self._drain_task.cancel()
            try:
                await self._drain_task
            except asyncio.CancelledError:
                pass
        if self._client is not None:
            await self._client.aclose()
