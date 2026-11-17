# ADR 0004 — AVIF Steganographic Watermarking via DCT

- **Status:** Accepted (implemented in `cmd/watermark/main.go` and `admin/service/`)

## Context
To trace unauthorized content leaks without writing temporary media files to disk, the platform requires an invisible watermark resilient to light re-encoding.

## Decision
Implement a dedicated serverless microservice (`cmd/watermark/main.go`) embedding frequency-domain DCT watermarks into AVIF images completely in RAM ("Zero Disk"), paired with a forensic CLI (`cmd/admin/main.go`) that extracts author, post, recipient, and leak timestamp metadata.

## Consequences
- Full leak attribution without visible image degradation.
- Internal S3 <-> Watermark service signatures use `security.GenerateHMAC` (strictly separate from public API Ratchet HMAC).
