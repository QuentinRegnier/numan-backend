---
name: continuous-learning
description: Capture lessons from resolved bugs, technical pitfalls, and architectural decisions to enrich docs/knowledge/, draft ADRs, and propose rule updates for human approval.
---

# Skill: `continuous-learning`

## When to Trigger
- After resolving a non-trivial bug or subtle regression (e.g., SQL/Scan mismatch, MsgPack quirk, worker deadlock, timezone/epoch-ms mismatch).
- When a recurring engineering pattern emerges across the codebase.
- When a significant architectural decision is introduced or revised.

## Capitalization Protocol

### 1. Autonomous Knowledge Capture
Whenever a technical pitfall or invariant is uncovered, add a concise entry to `docs/knowledge/common-pitfalls.md` documenting:
- **Exact Symptom** (error message, panic, or observed behavior).
- **Root Cause** (with links to affected files and symbols).
- **Fix & Prevention** (how to prevent recurrence in future tasks).

### 2. ADR Drafting (`docs/adr/`)
If a task introduces or changes a structural architectural decision (e.g., new Redis queue, storage tier change, real-time protocol update, serialization change), draft a numbered ADR `docs/adr/XXXX-<title>.md` (Context, Decision, Consequences) and flag it to the developer.

### 3. Strict Governance (Human-in-the-Loop)
- **Autonomous updates allowed:** `docs/knowledge/common-pitfalls.md` and `docs/knowledge/sql-signatures.md`.
- **Explicit human approval required:** Any edits to `AGENTS.md`, `.agents/rules/` (especially `05-security-cryptography.md` and `03-sql-and-persistence.md`), or security configurations. Propose the exact diff and wait for explicit approval.
