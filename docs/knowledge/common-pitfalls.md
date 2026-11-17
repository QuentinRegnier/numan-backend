# Known Technical Pitfalls (`common-pitfalls.md`)

This knowledge base records real issues observed in `numan-backend` to prevent regressions.

---

## 1. Column Count Mismatch Between `rows.Scan()`, `schema.sql`, and Test Mocks
- **Symptom:**
  `sql: expected 13 destination arguments in Scan, not 29` (observed in `internal/service/auth_service/add_certification_service_test.go`).
- **Root Cause:**
  `postgres.FuncLoadUser` (`internal/repository/postgres/func_load_user.go`) and view `auth.users_l3` (`schema.sql`) return and scan **29 columns**, while older `test.NewMockRows(...)` mocks (and a stale `"24-column shape"` comment in `schema.sql`) were not updated when 2FA, `trust_score`, and `last_infraction_at` columns were added.
- **Prevention:**
  Always run `make check-sql` (`python3 skills/schema-contract-validator/scripts/check_sql_scans.py`) and update all `FuncLoad*` test mocks whenever a column is added to a model or `schema.sql`.

## 2. Post Visibility Filter (`visibility >= 0` vs `visibility != 2`)
- **Symptom:**
  Friends-only posts (`visibility = 2`) disappear from queries and risk premature removal by orphan cleanup workers.
- **Root Cause:**
  In `schema.sql` (Decision D2), `-1` denotes soft-deleted posts whereas `2` denotes friends-only visibility.
- **Prevention:**
  Always filter active posts with `visibility >= 0`.

## 3. `pq.CopyIn` Incompatibility with `timestamptz`
- **Symptom:**
  PostgreSQL errors during worker batch flush on `int64` timestamp fields.
- **Root Cause:**
  `pq.CopyIn` serializes Go `int64` values as decimal text (e.g., `'1717600000000'`), which PostgreSQL rejects for `timestamptz`.
- **Prevention:**
  Every worker-persisted epoch-ms column must be `BIGINT` (`schema.sql` Decision D1).

## 4. Confusing Static Internal HMAC Signatures with Public API Ratchet HMAC
- **Root Cause:**
  `security.GenerateHMAC` (`internal/pkg/security/hmac_utils.go`) is strictly for internal service signatures (S3 / watermark microservice). Public API and WebSocket requests use rotating session Ratchet secrets in `internal/api/middleware/hmac.go`.
