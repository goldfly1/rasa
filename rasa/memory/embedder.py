"""CLI embedder: JSON-lines stdin/stdout protocol for OpenAI embeddings.

The Go memory controller spawns this as a long-running subprocess.
Protocol: one JSON request per stdin line → one JSON response per stdout line.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
from typing import Any

from openai import AsyncOpenAI
from tenacity import (
    retry,
    stop_after_attempt,
    wait_exponential,
    retry_if_exception_type,
)

MAX_TOKENS = 8191  # text-embedding-3-small limit


@retry(
    stop=stop_after_attempt(3),
    wait=wait_exponential(multiplier=1, min=1, max=8),
    retry=retry_if_exception_type(Exception),
)
async def _embed_batch(
    client: AsyncOpenAI, model: str, texts: list[str]
) -> list[list[float]]:
    resp = await client.embeddings.create(model=model, input=texts)
    return [d.embedding for d in resp.data]


async def embed_loop(model: str, api_key: str | None) -> None:
    """Read JSON lines from stdin, embed, write JSON lines to stdout."""
    client = AsyncOpenAI(api_key=api_key or os.environ.get("OPENAI_API_KEY"))

    reader = asyncio.StreamReader()
    loop = asyncio.get_running_loop()
    await loop.connect_read_pipe(
        lambda: asyncio.StreamReaderProtocol(reader), sys.stdin
    )

    w_transport, w_protocol = await loop.connect_write_pipe(
        asyncio.streams.FlowControlMixin, sys.stdout.fileno()
    )
    writer = asyncio.StreamWriter(w_transport, w_protocol, None, loop)

    while True:
        line = await reader.readline()
        if not line:
            break

        try:
            req: dict[str, Any] = json.loads(line.decode())
        except json.JSONDecodeError:
            continue

        request_id = req.get("request_id", "")
        texts: list[str] = req.get("texts", [])

        try:
            truncated = [t[:MAX_TOKENS] for t in texts]
            embeddings = await _embed_batch(client, model, truncated)
            resp = {"request_id": request_id, "embeddings": embeddings, "error": None}
        except Exception as exc:
            resp = {"request_id": request_id, "embeddings": None, "error": str(exc)}

        writer.write((json.dumps(resp) + "\n").encode())
        await writer.drain()


def main() -> None:
    parser = argparse.ArgumentParser(description="RASA Memory Embedder")
    parser.add_argument("--model", default="text-embedding-3-small")
    parser.add_argument("--api-key", default=None)
    _ = parser.parse_args()

    asyncio.run(embed_loop(_.model, _.api_key))


if __name__ == "__main__":
    main()
