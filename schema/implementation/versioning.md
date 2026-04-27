# Versioning

> **Architectural Reference:** `architectural_schema_v2.1.md` (cross-cutting)
> **Status:** Draft — pilot provisioning
> **Owner:** TBD

## 1. Purpose
Defines how implementation documents rev and link back to architecture changes. Prevents drift between boundary docs and component internals.

## 2. Rules
- When architecture changes, a ticket is cut to update all downstream implementation docs.
- When implementation discovers a boundary problem, the architecture doc is updated first.
- Implementation docs reference specific architecture § and version numbers.

### Pilot Addendum
All 14 implementation docs have been provisioned for pilot deployment (2026-04-25). Key changes from the enterprise architecture are documented in each doc's Change Log and cross-reference the relevant `top_level_decisions.md` § entries.

**Current baseline:** `architectural_schema_v2.1.md` — pilot provisioning round 1.

## 3. Change Log
| Date | Change | Author |
|------|--------|--------|
| 2026-04-25 | Added pilot provisioning addendum. All 14 implementation docs updated for native-process deployment. | Codex |
