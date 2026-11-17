# Real-Time & WebSocket Messaging

## Hub Architecture (`internal/api/websocket/`)
- Secured endpoint `/ws` (`websocket.ServeWS`) protected by `JWTMiddleware` + `HMACMiddleware`.
- Core components:
  - `hub.go`: central client registry and broadcast loop.
  - `client.go`: per-connection async read/write pumps (`gorilla/websocket`).
  - `router.go`: verifies frame Timestamp + HMAC (anti-replay & anti-bot) and dispatches frames to `ws_handlers/`.
  - `ws_handlers/`: `conversation_handler.go`, `message_handler.go`, `presence_handler.go`, `typing_handler.go`.
- Multi-node distribution via Redis Pub/Sub (`internal/service/realtime_service/distribution_service.go` / `BroadcastToConversation`).
- **Echo Prevention:** The HTTP originator is excluded from the corresponding WebSocket broadcast to prevent UI duplicates.

## Interface Contract
- WebSocket channels, events, and payload schemas are specified in `asyncapi.yaml` at the repository root.
