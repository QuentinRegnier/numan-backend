---
description: Git commit conventions, working tree protection, and sensitive file safeguards
globs: ["**/*"]
---

# Rule 07 — Git Workflow & Repository Hygiene

## 1. Commit Message Conventions
Follow Conventional Commits matching repository history:
- `feat(<scope>): <description>` (e.g., `feat(domain): ...`, `feat(map): ...`)
- `fix(<scope>): <description>`
- `refactor(<scope>): <description>`
- `docs(<scope>): <description>`
- `test(<scope>): <description>`
- `chore(<scope>): <description>`

## 2. Working Tree & Branch Safety
- Never run destructive Git commands (`git reset --hard`, `git clean -fd`, `git push --force`, `git checkout -- .`).
- Never modify, stage, or revert uncommitted user work outside the requested scope (such as `internal/templates/emails/` or external worktrees `agent/local`, `agent/cloud`).
- Check `git status` before and after edits to confirm only intended files changed.

## 3. Protected Files
- `k8s/01-config/secrets.env` and local secret/env files must never be exposed or committed.
- `wireguard/` local VPN configurations remain ignored by Git.
- `schema.sql` edits require immediate SQL/Go scan contract verification (`make check-sql`).
