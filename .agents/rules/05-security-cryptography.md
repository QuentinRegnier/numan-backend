---
description: Zero-trust cryptography, JWT, HMAC, Ratchet, TrustedProxies, and DCT steganography
globs: ["internal/api/middleware/**/*.go", "internal/api/websocket/**/*.go", "internal/pkg/security/**/*.go", "cmd/watermark/**/*.go", "admin/**/*.go", "internal/service/auth_service/**/*.go", "internal/service/two_factor_service/**/*.go"]
---

# Rule 05 — Security, Cryptography & Privacy

## 1. Zero-Trust Authentication & Integrity Chain
- All protected endpoints in `internal/api/routes.go` must sit under the `secured` group applying:
  1. `middleware.JWTMiddleware()` (identity & claims extraction)
  2. `middleware.HMACMiddleware()` (integrity, anti-replay, rotating session Ratchet)
- WebSocket frames routed by `internal/api/websocket/router.go` must enforce timestamp + HMAC verification against replay attacks and bots.
- Always perform constant-time HMAC comparisons using `hmac.Equal` (`internal/pkg/security/hmac_utils.go`).
- Never confuse:
  - `security.GenerateHMAC` (static internal signatures for S3 / watermark microservice).
  - Dynamic session Ratchet / public API & WebSocket security in `internal/api/middleware/hmac.go` and `internal/pkg/security/ratchet.go`.

## 2. HTTP Hardening & Edge Computing
- Preserve anti-Slowloris timeouts in `cmd/main.go` (`ReadTimeout`, `WriteTimeout`, `IdleTimeout`), `MaxBodySize()`, `RateLimiter()`, and `SetTrustedProxies(...)` against `X-Forwarded-For` spoofing.
- Ensure no `DeviceID` or internal credential leaks in HTTP responses.
- Validate edge-computed affinity vectors (`[224]float32` / bounds / HMAC signature) before storage or dot-product scoring.

## 3. Watermarking & Secrets Hygiene
- Never weaken DCT frequency-domain watermarking in `cmd/watermark/main.go`, `internal/service/media_service/media_watermark_service.go`, or `admin/service/`.
- Never commit real secrets, WireGuard private keys, or Firebase credentials (use `.template` or env files).
- Any modification to this rule or security middleware requires explicit human approval.
