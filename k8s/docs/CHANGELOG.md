# Changelog (decisions and changes)

## Revision 2: gateway removed, deployment templating added

### Removal of the nginx edge gateway (historical record)
The first revision of this repository introduced an `edge-gateway` nginx Deployment (`05-apps/edge-gateway.yaml`,
`05-apps/nginx-gateway.conf`) because the legacy `nginx.conf` contained an `internal` `/internal_minio/` location, the typical
X-Accel-Redirect pattern, which Traefik cannot process. The project owner has since confirmed that the Go application does **not** use
X-Accel-Redirect and streams media binaries itself from memory. The component was therefore removed completely: its Deployment, Service,
ServiceAccount, PodDisruptionBudget, generated ConfigMap, the two NetworkPolicies (`allow-gateway-to-backends`, `edge-gateway-egress`), the
Kustomize `resources`/`configMapGenerator` entries and all documentation. This is the only place where the old name is kept (this entry is
history); the validator additionally fails if any `gateway` object or the string `edge-gateway` reappears in the rendered manifests.

Resulting routing (all `Prefix`, no rewrites): `/` → `api:8080`, `/ws` → `api:8080`, `/process` → `watermark:8080`; `numan-http-redirect` keeps its
`redirectScheme` middleware (its `/` backend, never reached because the middleware answers 301, now points at `api:8080`).

Consequences, each deliberate:
* **Body limit.** Previously nginx limited every route to 20 MiB. Traefik's only mechanism is the `buffering` middleware, which also buffers
  responses; attaching it to `/` would put buffering in front of media streaming. It therefore stays on `/process` only. `/` and `/ws` now rely on
  the application's own limit (the legacy comment says 15 MB via MaxBytesReader). Verify it on every upload endpoint.
* **Anonymous read on the hot bucket** was justified by the unsigned nginx → MinIO fetch. `MINIO_HOT_ANONYMOUS_READ` now defaults to `false`; the
  bootstrap Job applies the value on every run (so it also removes a pre-existing anonymous policy). Set it to `true` if any consumer needs it.
* **Forwarded headers, TLS termination, HSTS, WebSocket upgrade** are unchanged (Traefik provides them directly).
* **Timeouts** were not changed (Traefik v3 defaults per its documentation: `readTimeout` 60 s, `writeTimeout` 0, `idleTimeout` 180 s). The earlier claim that Traefik
  applies no timeout to upgraded WebSocket connections was not verified and has been withdrawn from the documentation.

### Deployment templating
* 84 deployment variables in `infrastructure.env.template` (82 substituted, 2 CLI-only; 6 without a safe default).
* New: `Makefile`, `scripts/{render,envtool,lib,selftest}`, `tests/sample.env`, `docs/05-VARIABLES.md` (generated), `docs/06-TEMPLATING.md`.
* Rewritten: `scripts/deploy.sh` (renders, validates, guards the kube context, applies only rendered manifests), `scripts/validate.sh` (validates rendered output, writes a stamp).
* Changed: `scripts/verify.sh` (no gateway checks; the 21 MB test on `/` is now informational), `scripts/check-refs.py` (quota fit, Redis memory), `scripts/create-secrets.sh`
  (creates the namespace without reading a source file).
* Four shell snippets used brace expansions (`${BUCKET}`, `${COLD_DEFAULT_RETENTION_DAYS}`, `${target%%:*}`, `${target##*:}`); they were rewritten to be equivalent without braces so that
  the brace form is reserved for deployment variables. A latent defect found while doing so: an unquoted placeholder inside a YAML flow mapping in `hpa.yaml` (block style now).
* Behavioural identity: rendering with values equal to the previous literals gives the same Kubernetes objects except the intended changes (checked by diffing the old and new rendered output:
  65 of 72 shared objects identical; differences = ingress backends, rewritten shell snippets, image repository naming `<registry>/numan-api` and `<registry>/numan-watermark`).

## Revision 1
Initial repository: audit, architecture, manifests, scripts, documentation.
