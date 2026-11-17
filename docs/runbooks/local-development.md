# Runbook — Local Development

## Prerequisites
- Go 1.25+ (`/opt/homebrew/bin/go`)
- Docker & Docker Compose
- Local environment files (`k8s/infrastructure.env`, `k8s/01-config/secrets.env` derived from `k8s/infrastructure.env.template`)

## Quick Start
```bash
# Start full local stack (Traefik, API with Air hot-reload, Watermark, Redis, Postgres, Mongo, MinIO Hot/Cold, Mailpit)
make dev

# Stream container logs
make logs

# Stop containers (preserves local database volumes)
make down
```

## Useful Local Endpoints (`docker-compose.dev.yml`)
- **API via Traefik:** `http://localhost:80`
- **Traefik Dashboard:** `http://localhost:8080`
- **Mailpit UI:** `http://localhost/mailpit`
- **pgAdmin:** `http://localhost:8082`
- **Mongo Express:** `http://localhost:8084`
- **Redis Commander:** `http://localhost:8081`
- **MinIO Hot / Cold Consoles:** ports `9001` / `9003`
