from __future__ import annotations

import json
import time
import uuid
from dataclasses import dataclass, field
from typing import Any


@dataclass
class Metadata:
    soul_id: str = ""
    prompt_version_hash: str = ""
    agent_role: str = ""
    task_id: str = ""
    agent_id: str = ""
    timestamp_ms: int = 0

    def to_dict(self) -> dict[str, Any]:
        return {
            "soul_id": self.soul_id,
            "prompt_version_hash": self.prompt_version_hash,
            "agent_role": self.agent_role,
            "task_id": self.task_id,
            "agent_id": self.agent_id,
            "timestamp_ms": self.timestamp_ms,
        }

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> Metadata:
        return cls(
            soul_id=d.get("soul_id", ""),
            prompt_version_hash=d.get("prompt_version_hash", ""),
            agent_role=d.get("agent_role", ""),
            task_id=d.get("task_id", ""),
            agent_id=d.get("agent_id", ""),
            timestamp_ms=d.get("timestamp_ms", 0),
        )


@dataclass
class Envelope:
    message_id: str
    correlation_id: str
    source_component: str
    destination_component: str
    payload: dict[str, Any] = field(default_factory=dict)
    metadata: Metadata = field(default_factory=Metadata)

    @classmethod
    def new(
        cls,
        source: str,
        destination: str,
        payload: dict[str, Any] | None = None,
        metadata: Metadata | None = None,
        correlation_id: str | None = None,
    ) -> Envelope:
        return cls(
            message_id=str(uuid.uuid4()),
            correlation_id=correlation_id or str(uuid.uuid4()),
            source_component=source,
            destination_component=destination,
            payload=payload or {},
            metadata=metadata or Metadata(timestamp_ms=int(time.time() * 1000)),
        )

    def to_json(self) -> str:
        return json.dumps({
            "message_id": self.message_id,
            "correlation_id": self.correlation_id,
            "source_component": self.source_component,
            "destination_component": self.destination_component,
            "payload": self.payload,
            "metadata": self.metadata.to_dict(),
        })

    @classmethod
    def from_json(cls, raw: str | bytes) -> Envelope:
        d = json.loads(raw)
        return cls(
            message_id=d["message_id"],
            correlation_id=d["correlation_id"],
            source_component=d["source_component"],
            destination_component=d["destination_component"],
            payload=d.get("payload", {}),
            metadata=Metadata.from_dict(d.get("metadata", {})),
        )
