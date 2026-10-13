# Entry points for the Cloud CLI (Bash + Make). Every target is a thin wrapper around scripts/; the contract is:
#   exit 0 = success, non-zero = failure with a message on stderr; nothing is applied except by the deploy-* targets.
# Select the environment file with ENV_FILE (default infrastructure.env, copied from infrastructure.env.template).
SHELL := /bin/bash
ENV_FILE ?= infrastructure.env
OUT      ?= build
export ENV_FILE

.PHONY: help lint env-check render render-strict validate diff deploy deploy-tls deploy-admin deploy-traefik verify inventory test clean
help: ## list targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | sed 's/:.*## /\t/'

lint: ## template/variable consistency (T == E), syntax rules, collisions
	@python3 scripts/envtool.py lint
env-check: ## validate $(ENV_FILE) only (types, ranges, cross-field rules)
	@python3 scripts/envtool.py check-env $(ENV_FILE)
render: ## compile templates -> $(OUT)/manifests (no cluster access, no apply)
	@bash scripts/render.sh --env-file $(ENV_FILE) --out $(OUT)
render-strict: ## same, but reject reserved placeholder domains (what deploy uses)
	@bash scripts/render.sh --env-file $(ENV_FILE) --strict --out $(OUT)
validate: ## schema + consistency checks of the rendered output (run render first)
	@bash scripts/validate.sh --out $(OUT)
diff: render-strict validate ## show what deploy would change (kubectl diff against KUBE_CONTEXT)
	@kubectl --context "$$(grep '^KUBE_CONTEXT=' $(OUT)/resolved.env | cut -d= -f2-)" diff -f $(OUT)/manifests/core.yaml || test $$? -le 1
deploy: ## render + validate + context guard + diff + confirm + apply + wait (CONFIRM with --yes via ARGS=--yes)
	@bash scripts/deploy.sh --env-file $(ENV_FILE) --stack core $(ARGS)
deploy-tls: ## ClusterIssuers + Certificate (cert-manager must be installed)
	@bash scripts/deploy.sh --env-file $(ENV_FILE) --stack tls $(ARGS)
deploy-admin: ## optional admin UIs (still replicas 0)
	@bash scripts/deploy.sh --env-file $(ENV_FILE) --stack admin $(ARGS)
deploy-traefik: ## optional cluster-wide Traefik tuning (kube-system)
	@bash scripts/deploy.sh --env-file $(ENV_FILE) --stack traefik $(ARGS)
verify: ## smoke tests against the live cluster (HOST=<domain> adds external checks)
	@bash scripts/verify.sh
inventory: ## regenerate docs/05-VARIABLES.md from the real sources
	@python3 scripts/envtool.py inventory > docs/05-VARIABLES.md
test: ## self-test of the templating system with non-secret sample values (no cluster needed)
	@bash scripts/selftest.sh
clean: ## remove build artifacts
	@rm -rf $(OUT)
