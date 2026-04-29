"""Regex-based secret/PII scanner for agent output validation.

Upgrade path: Semgrep + detect-secrets for multi-language pattern matching.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

# Ordered list of (name, pattern, severity, action)
# deny = block promotion, warn = log only
RULES: list[dict[str, Any]] = [
    {
        "name": "aws_access_key",
        "pattern": re.compile(r"AKIA[0-9A-Z]{16}"),
        "severity": "high",
        "action": "deny",
    },
    {
        "name": "private_key",
        "pattern": re.compile(r"-----BEGIN\s.*PRIVATE\sKEY-----"),
        "severity": "high",
        "action": "deny",
    },
    {
        "name": "generic_api_key",
        "pattern": re.compile(r"""(?ix)(api[_-]?key|apikey)\s*[:=]\s*["'][A-Za-z0-9_\-]{16,}["']"""),
        "severity": "high",
        "action": "deny",
    },
    {
        "name": "secret_token",
        "pattern": re.compile(r"""(?ix)(secret|token|auth[_-]?token)\s*[:=]\s*["'][A-Za-z0-9_\-\.]{16,}["']"""),
        "severity": "high",
        "action": "deny",
    },
    {
        "name": "hardcoded_password",
        "pattern": re.compile(r"""(?ix)password\s*[:=]\s*["'][^"'\s]{4,}["']"""),
        "severity": "medium",
        "action": "warn",
    },
    {
        "name": "connection_string",
        "pattern": re.compile(r"""(?ix)(mongodb|postgresql|mysql|redis|jdbc)://[^\s"']+"""),
        "severity": "high",
        "action": "deny",
    },
]


@dataclass
class ScanResult:
    passed: bool
    findings: list[dict[str, Any]] = field(default_factory=list)


def scan_file(path: Path) -> ScanResult:
    """Scan a single file for secrets. Returns ScanResult with findings."""
    findings: list[dict[str, Any]] = []

    try:
        content = path.read_text(encoding="utf-8", errors="ignore")
    except Exception:
        findings.append({
            "rule": "read_error",
            "file": str(path),
            "severity": "low",
            "line": 0,
            "match": f"could not read {path}",
            "action": "warn",
        })
        return ScanResult(passed=True, findings=findings)

    for lineno, line in enumerate(content.splitlines(), start=1):
        for rule in RULES:
            m = rule["pattern"].search(line)
            if m:
                findings.append({
                    "rule": rule["name"],
                    "file": str(path),
                    "severity": rule["severity"],
                    "line": lineno,
                    "match": m.group(0),
                    "action": rule["action"],
                })

    passed = not any(f["action"] == "deny" for f in findings)
    return ScanResult(passed=passed, findings=findings)


def scan_directory(root: str | Path) -> ScanResult:
    """Scan all text files in a directory tree."""
    root = Path(root)
    all_findings: list[dict[str, Any]] = []
    deny_count = 0

    for p in root.rglob("*"):
        if not p.is_file():
            continue
        if p.suffix in {".exe", ".dll", ".pyd", ".so", ".obj", ".pyc", ".pyo",
                        ".zip", ".tar", ".gz", ".7z", ".jpg", ".png", ".gif", ".ico"}:
            continue
        if p.stat().st_size > 1_000_000:  # skip files > 1MB
            continue

        result = scan_file(p)
        all_findings.extend(result.findings)
        if not result.passed:
            deny_count += 1

    return ScanResult(
        passed=deny_count == 0,
        findings=all_findings,
    )
