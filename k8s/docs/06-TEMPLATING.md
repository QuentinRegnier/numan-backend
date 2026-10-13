# Deployment templating: convention, workflow, troubleshooting

This repository separates two kinds of variables. Mixing them up is the main way such a system breaks.

| | Deployment-time variables | Application runtime variables |
|---|---|---|
| Examples | `NUMAN_DOMAIN`, `STORAGE_CLASS`, `PVC_SIZE_POSTGRES`, `IMAGE_REGISTRY`, `API_IMAGE_TAG` | `JWT_SECRET`, `DB_DSN`, `REDIS_PASSWORD`, `WATERMARK_SECRET_KEY`, `NODE_ID` |
| Resolved by | the Cloud CLI render step, **before** `kubectl` sees anything | the container, at start, from the pod's environment |
| Declared in | `infrastructure.env.template` (84 variables) | ConfigMaps/Secrets/`env:` in the manifests (`01-config/`, `05-apps/`) |
| Secret? | never (the linter rejects secret-looking names) | secrets live in Kubernetes Secrets, created by `scripts/create-secrets.sh` |
| Appears in rendered YAML | as the substituted value | untouched |

## 1. Syntax rules (enforced by `make lint`)

| Form | Meaning | Who expands it |
|---|---|---|
| `${NAME}` | **deployment variable** (reserved for this purpose) | `envsubst` with an explicit allowlist |
| `$(NAME)` | Kubernetes runtime reference (e.g. in `DB_DSN`) | kubelet, at pod start; `envsubst` never touches it |
| `$NAME` | shell variable inside a container script (backup/init Jobs, Redis args) | the container's shell |
| `$$`, `${name%%x}` and any other brace form | not allowed in templates | lint error |

Consequences: any `${...}` found in a template must be a plain variable name defined in `infrastructure.env.template`; a bare `$NAME` equal to a deployment variable is an error (it would be substituted); a deployment variable may not share its name with any ConfigMap key, container `env:` name or Secret key. String values are written quoted (`"${NUMAN_DOMAIN}"`); integers and booleans unquoted (`replicas: ${API_MIN_REPLICAS}`), and they are type-checked before substitution. Never put a placeholder inside a YAML flow mapping (`{a: ${X}}`): lint parses every template as YAML and fails.

## 2. The environment file

`infrastructure.env.template` is the blueprint. Copy it to `infrastructure.env` (git-ignored), or point `ENV_FILE` at any file (for example one per cluster). Rules:

* conventional dotenv: `NAME=value`, no `export`, no inline comments, no interpolation; parsed by `scripts/envtool.py`, **never sourced**.
* variables you omit take the template default; the render prints how many did. Variables whose value is `__REQUIRED__` have no safe default and **must** be set (6 today: `KUBE_CONTEXT`, `NUMAN_DOMAIN`, `ACME_EMAIL`, `IMAGE_REGISTRY`, `API_IMAGE_TAG`, `WATERMARK_IMAGE_TAG`). A CLI that turns this file into prompts should ask for exactly those.
* unknown names, duplicates, forbidden characters (`$ ` " ' \ #`, control characters), wrong types and inconsistent combinations are errors, with the variable name and the constraint in the message.
* every variable carries machine-readable annotations (`purpose`, `type`, `required`, `scope`, `env-specific`, `constraints`, `depends`, `change-impact`, `allow-empty`); the full table is generated into [05-VARIABLES.md](05-VARIABLES.md).
* two variables are `scope: cli` (`KUBE_CONTEXT`, `KUBERNETES_VERSION`): used by the scripts, never substituted into manifests. Everything else is `scope: template`.

Types validated: `int:MIN:MAX`, Kubernetes `quantity` (the real grammar: `250m`, `1Gi`, `4608Mi`, `1e3`), `bool`, `cron`, `timezone` (checked against the operator machine's tz database), `hostname`, `email`, `image-registry`, `image-tag` (`latest` rejected), `image-ref` (tag or digest required), `enum`, `bucket`, `identifier`, `k8s-name`, `semver`, `retention-mode`.
Cross-field rules: CPU/memory request ≤ limit (every workload), HPA min ≤ max and ≥ 1, `API_MAX_REPLICAS` < 11 (worker NODE_IDs start at 11), object-lock/retention consistency, quota request ≤ limit, sum of `PVC_SIZE_*` ≤ storage quota. After rendering, `check-refs.py` also checks that the declared workloads fit the ResourceQuota and that the Redis memory limit covers `REDIS_MAXMEMORY`.

## 3. Workflow

```
make lint            T == E: every ${VAR} used by a template is defined, every defined variable is used, syntax rules hold
make render          lint → validate env file → substitute (allowlist, clean environment) → check nothing unresolved →
                     parse every file as YAML → kustomize build the RENDERED tree → write build/ + hash stamp
make validate        stamp check → kubeconform --strict → cross-references, quota/Redis checks → secret scan → (optional) image check
make diff            render --strict + validate + kubectl diff against KUBE_CONTEXT          (read-only)
make deploy          render --strict + validate + context guard + diff + confirmation + apply + rollout waits
make deploy-tls | deploy-admin | deploy-traefik          the separate stacks (cert-manager / admin UIs / cluster-wide Traefik)
```
`make deploy ARGS=--dry-run` is a server-side dry run; `ARGS=--yes` skips the confirmation prompt (the context guard still applies). All targets take `ENV_FILE=...`; the build directory is `build/` (`OUT=`).

How substitution is made safe:

1. `envsubst` receives an **explicit allowlist** (`${A} ${B} …`, generated from the template) and runs under `env -i`, so nothing from the operator's shell can influence the output and no other `$…` is touched.
2. Values are validated first and cannot contain `$`, backticks, quotes or backslashes; they are never re-expanded; nothing is `eval`ed or sourced.
3. After substitution the renderer fails if any `${NAME}`, `__REQUIRED__` or the guard marker remains, and every rendered file must parse as YAML.
4. Output is deterministic (sorted file order, no timestamps; tested by `make test`: two renders hash identically) and the sources are never modified (also tested).

## 4. Kustomize integration

Kustomize does **not** expand `${VAR}`, and `kubectl apply -k` does no substitution. Therefore the order is fixed: **render the whole tree first (including `kustomization.yaml`), then `kustomize build` the rendered copy** in `build/rendered/`. The templated parts of Kustomize itself are the `images:` transformer (`${IMAGE_REGISTRY}/numan-api`, `${API_IMAGE_TAG}`) and the storage-class JSON patch (`${STORAGE_CLASS}`). Rendering first also means a bad value fails before any Kustomize error could hide it.

The unrendered tree is deliberately unbuildable: the first resource in each template `kustomization.yaml` is `__TEMPLATE_NOT_RENDERED__run-make-render-first.yaml` (a file that does not exist), so `kustomize build .` or `kubectl apply -k .` on the sources fails with that name in the message. The renderer removes this single line from the rendered copy. Outputs: `build/manifests/{core,tls,admin,traefik}.yaml`.

## 5. Secrets and artifacts

* No secret is ever a deployment variable. Runtime secrets stay in Kubernetes Secrets created from the git-ignored `01-config/secrets.env` by `scripts/create-secrets.sh` (values go through temp files with mode 600, never command-line arguments). `01-config/secret-templates.yaml` is excluded from rendering.
* Rendered manifests contain **no secret values by construction** (they reference Secrets by name; `DB_DSN` uses `$(POSTGRES_PASSWORD)` references). `validate.sh` additionally scans them for secret-looking literals. They still are build artifacts: `build/`, `infrastructure.env` and `environments/*.env` are in `.gitignore`.
* `build/resolved.env` (mode 600) records the fully resolved, non-secret variable set for audit.
* The render prints variable **names** and constraint messages on errors, never values of secrets (there are none in this file by design). Temporary files live in `build/` (created with `umask 077`) or `mktemp` directories that are removed on exit.
* Only `deploy.sh` applies anything, and only `build/manifests/<stack>.yaml` after a fresh render and validation in the same invocation, a matching hash stamp, and a kube-context equal to `KUBE_CONTEXT`.

## 6. Operational notes for specific variables

* **Immutable fields:** `STORAGE_CLASS` and every `PVC_SIZE_*` live in `volumeClaimTemplates`, which Kubernetes does not allow to change on an existing StatefulSet. Changing them later does not resize or migrate volumes (and shrinking is impossible; expansion needs a StorageClass with `allowVolumeExpansion`, which `local-path` lacks). Plan a backup → recreate → restore.
* **Replica counts:** `API_MIN_REPLICAS` and `WATERMARK_MIN_REPLICAS` are written both to the workload and to the HPA minimum, so every apply resets the workload to the minimum before the HPA scales it up again. Removing the `replicas` field would avoid that but is a behaviour change not made here.
* **`MINIO_HOT_ANONYMOUS_READ`:** the default is now `false`. The legacy script enabled anonymous read because of the nginx X-Accel pattern; the application streams media itself, so nothing needs it. The Job re-applies the value on each run, so `false` also removes an anonymous policy from an existing bucket. If anything reads unsigned URLs, set `true`.
* **`BACKUP_RETENTION_*`:** lowering them permanently deletes older snapshots at the next backup run.
* **`SYSTEM_TIMEZONE`:** validated against the operator machine's tz database; the cluster's controller database may differ.
* **Image naming:** application images are `<IMAGE_REGISTRY>/numan-api:<API_IMAGE_TAG>` (also used by the worker) and `<IMAGE_REGISTRY>/numan-watermark:<WATERMARK_IMAGE_TAG>`.
* **Intentionally NOT variables:** namespace (`numan` appears in policies, middleware references and FQDNs), Service names and ports, label keys, API versions, third-party image versions (pinned in manifests), TLS Secret name, probe/timeouts, NetworkPolicy contents, application runtime settings in `numan-config` (worker batch/backoff, `REDIS_MAXMEMORY`) which keep their runtime names.

## 7. Cloud CLI integration contract

The actual Cloud CLI sources were **not** available, so compatibility is by contract rather than inspection: every target is a plain Make/Bash entry point; success is exit code 0, failure is non-zero with the message on stderr (`ENV ERROR:` / `LINT ERROR:` prefixes are stable and machine-greppable); `infrastructure.env.template` is parseable with simple line rules (`NAME=value`, annotation comments `# key: value` directly above each variable, `__REQUIRED__` sentinel); `scripts/envtool.py resolved FILE` prints the resolved set. Required tools on the machine that renders: `bash`, `python3` ≥ 3.9 + PyYAML, `envsubst` (gettext), `kustomize` (or `kubectl`), `kubeconform` (validation), `make`.

## 8. Troubleshooting

| Message | Cause → fix |
|---|---|
| `ENV ERROR: X: required value missing (still __REQUIRED__)` | set it in the env file |
| `ENV ERROR: unknown variable X` | typo, or the variable was never added to the template |
| `ENV ERROR: X: not a valid Kubernetes quantity: '1GB'` | use `1Gi`/`1G`; `GB` is not a unit |
| `ENV ERROR: X (4) must be <= X_LIMIT (2)` | request above limit |
| `ENV ERROR: … reserved/placeholder domain; refusing to deploy` | strict mode (used by `deploy`); use a real domain/registry |
| `LINT ERROR: T\E: ${X} is used … but not defined` | a manifest uses a variable missing from the template |
| `LINT ERROR: E\T: X is defined … but never used` | remove the variable or use it |
| `LINT ERROR: ${…} is not a plain deployment variable name` | brace form is reserved; use `$NAME` for shell, `$(NAME)` for Kubernetes |
| `LINT ERROR: … is not valid YAML before substitution` | an unquoted placeholder in a flow mapping or similar |
| `unresolved deployment placeholder(s) in rendered output` | a token survived substitution (not in the allowlist) |
| `rendered files changed after rendering (stamp mismatch)` | something edited `build/`; re-run `make render` |
| `current kubectl context is 'a' but KUBE_CONTEXT is 'b'` | switch context or fix `KUBE_CONTEXT` |
| `accumulating resources … __TEMPLATE_NOT_RENDERED__…` | you built the template tree; build `build/rendered` via `make render` |
| `ResourceQuota … is smaller than the declared workloads` | raise `QUOTA_*` or lower replicas/requests |
| `envsubst: command not found` | install gettext (`gettext-base` on Debian/Ubuntu, `brew install gettext`) |
