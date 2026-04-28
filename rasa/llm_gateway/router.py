"""Tier → provider + model resolution from gateway.yaml."""

from __future__ import annotations

import os
import re
from pathlib import Path

import yaml

_ENV_VAR_RE = re.compile(r"\$\{(\w+)(?::-(.*?))?\}")


def _resolve_env(value: str) -> str:
    """Expand ${VAR} and ${VAR:-default} patterns in a string."""

    def _repl(m: re.Match) -> str:
        var, default = m.group(1), m.group(2)
        return os.environ.get(var, default or "")

    return _ENV_VAR_RE.sub(_repl, value)


class TierRouter:
    """Maps budget tiers to concrete (provider_name, model_id) pairs."""

    def __init__(self, config_path: str | Path = "config/gateway.yaml") -> None:
        raw = Path(config_path).read_text()
        cfg = yaml.safe_load(raw)
        self._tiers: dict[str, dict[str, str]] = cfg.get("tiers", {})
        self._providers: dict[str, dict] = cfg.get("providers", {})
        self._cache_cfg: dict = cfg.get("cache", {})
        self._fallback_cfg: dict = cfg.get("fallback", {})

    def resolve(self, tier: str | None) -> tuple[str, str]:
        """Return (provider_name, model_id) for a tier. Falls back to standard."""
        t = tier or "standard"
        if t in self._tiers:
            entry = self._tiers[t]
            return entry.get("provider", "ollama"), _resolve_env(entry.get("model", ""))
        # Fallback: first available tier
        if self._tiers:
            first = next(iter(self._tiers.values()))
            return first.get("provider", "ollama"), _resolve_env(first.get("model", ""))
        return "ollama", ""

    @property
    def tiers(self) -> list[str]:
        """Ordered list of tier names (for fallback chain)."""
        return list(self._tiers.keys())

    @property
    def cache_ttl(self) -> int:
        return int(self._cache_cfg.get("ttl", 3600))

    @property
    def fallback_enabled(self) -> bool:
        return bool(self._fallback_cfg.get("enabled", True))

    @property
    def max_attempts(self) -> int:
        return int(self._fallback_cfg.get("max_attempts", 3))

    @property
    def backoff_ms(self) -> int:
        return int(self._fallback_cfg.get("backoff_ms", 1000))
