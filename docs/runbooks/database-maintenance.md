# Runbook — Database Maintenance

## Full Local Reset (Hard Reset)
> ⚠️ **Warning:** Destroys all local Docker volumes (PostgreSQL, MongoDB, Redis, MinIO).
```bash
make clean
```

## PostgreSQL L3 Schema (`schema.sql`)
- `schema.sql` is a fresh-install bootstrap script, not an in-place production upgrade migration script.
- After any edit to `schema.sql`, verify Go repository compatibility via:
```bash
make check-sql
```

## Redis AOF Safety on Startup
- In `cmd/main.go`, database wipe on startup only executes when `CLEAN_DB_ON_STARTUP=true`. By default, Redis AOF and queued events in `WorkerQueue` are preserved.
- If Redis starts empty (`ZCard(RedisKeyStrictRecent) == 0`), `cmd/main.go` automatically triggers full cache seeding (`SeedMostCache`, `SeedGraphCache`, `SeedSpeedCache`, `SeedUserCache`).
