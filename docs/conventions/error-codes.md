# Error Codes Registry (`numan_error`)

Single source of truth: `internal/domain/numan_error/app_error.go`.

## Standard Codes
- `CodeInternalError` (`INTERNAL_SERVER_ERROR`) — `500`
- `CodeDatabaseError` (`DATABASE_ERROR`) — `500`
- `CodeCacheError` (`CACHE_ERROR`) — `500`
- `CodeNotFound` (`RESOURCE_NOT_FOUND`) — `404`
- `CodeForbidden` (`FORBIDDEN_ACCESS`) — `403`
- `CodeUnauthorized` (`UNAUTHORIZED`) — `401`
- `CodeInvalidPayload` (`INVALID_PAYLOAD`) — `400`
- `CodeConflict` (`RESOURCE_CONFLICT`) — `409`
- `CodeTooManyRequests` (`TOO_MANY_REQUESTS`) — `429`
- `CodePayloadTooLarge` (`PAYLOAD_TOO_LARGE`) — `413`
- `CodeUnsupportedMedia` (`UNSUPPORTED_MEDIA_TYPE`) — `415`
- `CodeServiceUnavailable` (`SERVICE_UNAVAILABLE`) — `503`

## Domain / Security / Moderation Codes
- `CodeTelemetrySyncRequired` (`TELEMETRY_SYNC_REQUIRED`)
- `CodeMissingFile` (`MISSING_FILE`), `CodeFileReadError` (`FILE_READ_ERROR`)
- `CodeUserIsNotIdentified` (`USER_NOT_IDENTIFIED`)
- `CodeTwoFactorRequired` (`TWO_FACTOR_REQUIRED`), `CodeTwoFactorInvalid` (`TWO_FACTOR_INVALID_PROOF`), `CodeTwoFactorExpired` (`TWO_FACTOR_SESSION_EXPIRED`)
- `CodePasskeyCloneAlert` (`PASSKEY_CLONE_DETECTED`)
- `CodeUserBanned` (`USER_BANNED`)
- `CodeMediaUnderReview` (`MEDIA_UNDER_REVIEW`), `CodeMediaRejected` (`MEDIA_REJECTED`)
- `CodeSteganoBanned` (`STEGANO_IDENTITY_BANNED`)

> **Rule:** Never hardcode error code strings outside `app_error.go` and never expose `appErr.Err` in public HTTP error responses.
