# Edge Computing, Affinity Vectors & HMAC Security

## Offloaded Profiling (Edge Computing)
- Implicit and explicit behavioral signals are aggregated client-side via Exponential Moving Average (EMA) into a dense `[224]float32` affinity vector.
- The backend validates the submitted vector and executes similarity / LSH filtering (`internal/service/algorithm_service/`, `lsh_service.go`, `vectorization_service.go`), alongside Cuckoo Filters for O(1) RAM uniqueness checks.

## HTTP Security Chain (`internal/api/routes.go` & `middleware/`)
1. **Global Hardening:**
   - `SetTrustedProxies` (private Docker/Nginx networks only, preventing `X-Forwarded-For` spoofing).
   - `TraceIDMiddleware` & `CustomRecoveryMiddleware`.
   - Anti-Slowloris HTTP server timeouts (`ReadTimeout: 5s`, `WriteTimeout: 10s`, `IdleTimeout: 15s`).
   - `MaxBodySize()`, `RateLimiter()`, `CORSMiddleware()`.
2. **`secured` Route Group:**
   - `middleware.JWTMiddleware()`: user authentication and claims extraction.
   - `middleware.HMACMiddleware()`: request integrity (`Method|Path|Ts|Body`), anti-replay, and rotating session Ratchet (`internal/pkg/security/ratchet.go`).

## DCT Steganographic Watermarking
- `cmd/watermark/main.go` embeds frequency-domain DCT watermarks into AVIF images entirely in RAM ("Zero Disk") with author, post, recipient, and timestamp metadata, extractable offline via `cmd/admin/main.go`.
