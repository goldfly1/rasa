"""LLM Gateway — Unified provider client with tiered routing, caching, resilience.

Reads configuration from config/gateway.yaml tier mapping.
"""

import os
import json
import httpx
from typing import Any
from tenacity import retry, stop_after_attempt, wait_exponential

from rasa.db.conn import get_pool

TIER_PRIORITY = {"premium": 0, "standard": 1}


class GatewayError(Exception):
    pass


class GatewayClient:
    """Async HTTP client for Ollama (OpenAI-compatible API) and future providers."""

    def __init__(self) -> None:
        cfg = _gateway_config()
        self._providers: dict[str, dict[str, Any]] = cfg["providers"]
        self._tiers: dict[str, str] = cfg["tiers"]
        self._fallback = cfg["fallback"]
        self._cache = get_pool("rasa_memory")  # psycopg pool with JSONB support

    async def complete(
        self,
        prompt: str,
        tier: str | None = None,
        model: str | None = None,
        temperature: float | None = None,
        max_tokens: int | None = None,
        extra_body: dict | None = None,
    ) -> dict[str, Any]:
        """Send a chat completion to the appropriate provider."""
        # Resolve tier -> provider
        resolved = self._resolve_tier(tier, model)
        provider = self._providers[resolved["provider"]]
        url = provider["base_url"] + "/chat/completions"
        headers = {"Authorization": f"Bearer {provider['api_key']}", "Content-Type": "application/json"}
        payload: dict[str, Any] = {
            "model": resolved["model"],
            "messages": [{"role": "user", "content": prompt}],
            "stream": False,
        }
        if temperature is not None:
            payload["temperature"] = temperature
        if max_tokens is not None:
            payload["max_tokens"] = max_tokens
        if extra_body:
            payload.update(extra_body)

        for attempt in range(self._fallback["max_attempts"]):
            try:
                async with httpx.AsyncClient(timeout=httpx.Timeout(provider.get("timeout", 120))) as c:
                    r = await c.post(url, headers=headers, json=payload)
                    r.raise_for_status()
                    data = r.json()
                    await self._cache_response(prompt, tier, data)
                    return {"content": data["choices"][0]["message"]["content"], "model": data["model"], "usage": data.get("usage", {})}
            except Exception as exc:
                if attempt == self._fallback["max_attempts"] - 1:
                    raise GatewayError(f"Provider failure after {self._fallback['max_attempts']} attempts: {exc}") from exc
                await asyncio.sleep(self._fallback["backoff_ms"] / 1000 * (2 ** attempt))

        raise GatewayError("Unreachable")  # type: ignore[unreachable]

    def _resolve_tier(self, tier: str | None, model: str | None) -> dict[str, Any]:
        """Map tier / model override to a concrete provider entry."""
        if model:
            for name, p in self._providers.items():
                if p["model"] == model:
                    return {"provider": name, **p}
        rank = TIER_PRIORITY.get(tier or "standard", 1)
        candidates = [c for c in self._providers.items() if TIER_PRIORITY.get(c[1].get("weight", 1), 99) >= rank]
        if not candidates:
            candidates = list(self._providers.items())
        # Simple load-balance: pick max-weight candidate for now
        pick = max(candidates, key=lambda kv: kv[1].get("weight", 0))
        return {"provider": pick[0], **pick[1]}

    async def _cache_response(self, prompt: str, tier: str, response: dict) -> None:
        """Insert a cache entry into rasa_memory for deduplication and tracing."""
        resolved = self._resolve_tier(tier, None)
        try:
            async with self._cache.connection() as conn:
                await conn.execute(
                    "INSERT INTO embeddings (id, node_id, model, chunk_text, embedding) VALUES (gen_random_uuid(), 'llm-cache', %s, %s, NULL)",
                    (resolved["model"], json.dumps(prompt)[:2000], "[0.0]*768"),
                )
        except Exception:
            pass  # Cache is best-effort

    async def models(self) -> list[dict[str, Any]]:
        """List available local Ollama models."""
        url = self._providers["ollama"]["base_url"].replace("/v1", "") + "/api/tags"
        async with httpx.AsyncClient(timeout=httpx.Timeout(10)) as c:
            r = await c.get(url)
            r.raise_for_status()
            return r.json().get("models", [])


def _gateway_config() -> dict[str, Any]:
    import yaml
    with open("config/gateway.yaml") as f:
        return yaml.safe_load(f)
