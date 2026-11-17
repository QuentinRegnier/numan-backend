---
name: api-docs-sync
description: Regenerate and verify OpenAPI/Swagger and AsyncAPI (WebSocket) documentation after modifying routes, handlers, or I/O models.
---

# Skill: `api-docs-sync`

## When to Trigger
- Adding, modifying, or removing a route in `internal/api/routes.go`.
- Updating Swaggo annotations (`@Summary`, `@Param`, `@Success`, `@Failure`, `@Router`) in `internal/api/handlers/`.
- Modifying an API-exposed I/O model in `internal/domain/models/`.
- Modifying WebSocket events in `internal/api/websocket/` (verify `asyncapi.yaml`).

## Execution Steps

### 1. Regenerate Local Swagger Documentation
Prefer the local `swag` binary in `$(go env GOPATH)/bin/swag` (faster and offline-safe):
```bash
if [ -x "$(go env GOPATH)/bin/swag" ]; then
  "$(go env GOPATH)/bin/swag" init -g cmd/main.go -d . --parseDependency --parseInternal
else
  make docs
fi
```

### 2. Verify Route Security & Error Annotations
- Confirm protected endpoints in `internal/api/routes.go` belong to the `secured` group (`JWTMiddleware` + `HMACMiddleware`).
- Confirm Swagger annotations accurately document required headers (`Authorization`, `X-Signature`, `X-Timestamp`) and actual `numan_error.PublicErrorResponse` status codes.

### 3. Verify WebSocket AsyncAPI Contract (`asyncapi.yaml`)
- When modifying `internal/api/websocket/ws_handlers/` or `internal/api/websocket/models.go`, update `asyncapi.yaml` to match channels, event names, and payload schemas.

## References
- `internal/api/routes.go`
- `asyncapi.yaml`
- `docs/swagger-audit/`
