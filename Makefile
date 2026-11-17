# ==============================================================================
# MAKEFILE NUMAN BACKEND - DÉVELOPPEMENT LOCAL
# ==============================================================================

# Variables pour centraliser l'appel à Docker Compose avec les bons environnements
ENV_ARGS = --env-file k8s/infrastructure.env --env-file k8s/01-config/secrets.env
COMPOSE  = docker compose $(ENV_ARGS) -f docker-compose.dev.yml

.PHONY: help docs dev down logs test lint check-sql check clean

default: help

# ------------------------------------------------------------------------------
# 1. DÉVELOPPEMENT ET EXÉCUTION
# ------------------------------------------------------------------------------

help: ## Affiche la liste des commandes disponibles
	@echo "=================================================="
	@echo "🛠️  NUMAN BACKEND - LOCAL DEV ENVIRONMENT"
	@echo "=================================================="
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

docs: ## Génère la documentation Swagger (Met à jour le dossier docs/)
	@echo "📚 Génération de la documentation Swagger..."
	go run github.com/swaggo/swag/cmd/swag@latest init -g cmd/main.go -d . --parseDependency --parseInternal
	@echo "✅ Swagger à jour."

dev: docs ## Lance l'environnement local complet (Hot-Reload activé via Air)
	@echo "🚀 Démarrage de l'infrastructure de développement..."
	$(COMPOSE) up -d --build --remove-orphans
	@echo "✅ Numan est en ligne ! Modifie ton code Go, Air rechargera l'API automatiquement."

down: ## Arrête les conteneurs locaux (Conserve les données en base)
	@echo "🛑 Arrêt en cours..."
	$(COMPOSE) down

logs: ## Affiche les logs de tous les conteneurs locaux en direct
	@echo "📋 Lecture des logs (Ctrl+C pour quitter)..."
	$(COMPOSE) logs -f

# ------------------------------------------------------------------------------
# 2. OUTILS, QUALITÉ ET NETTOYAGE
# ------------------------------------------------------------------------------

test: ## Lance tous les tests unitaires du projet Go
	@echo "🧪 Exécution des tests..."
	go test -v ./...

lint: ## Exécute go vet et staticcheck (si compilé pour Go 1.25+) sur tout le module Go
	@echo "🔍 Analyse statique (go vet)..."
	go vet ./...
	@SC_BIN="$$(command -v staticcheck || echo "$$(go env GOPATH)/bin/staticcheck")"; \
	if [ -x "$$SC_BIN" ]; then \
		SC_GO=$$(go version -m "$$SC_BIN" 2>/dev/null | awk 'NR==1{print $$2}'); \
		if echo "$$SC_GO" | grep -qE '^go1\.(2[5-9]|[3-9][0-9])'; then \
			echo "🔍 Exécution de staticcheck ($$SC_GO)..."; \
			"$$SC_BIN" ./...; \
		else \
			echo "⚠️ staticcheck ($$SC_GO) est plus ancien que go1.25.0 ; relancez 'go install honnef.co/go/tools/cmd/staticcheck@latest' pour l'activer."; \
		fi; \
	else \
		echo "⚠️ staticcheck introuvable dans PATH ou GOPATH/bin, ignoré."; \
	fi

check-sql: ## Vérifie l'alignement entre schema.sql et les rows.Scan() Go
	@python3 skills/schema-contract-validator/scripts/check_sql_scans.py

check: lint check-sql test ## Lance l'analyse statique, le contrôle SQL et les tests unitaires

clean: down ## ⚠️ Arrête et DÉTRUIT les bases de données locales (Hard Reset)
	@echo "🧹 Suppression des conteneurs, réseaux et volumes locaux..."
	$(COMPOSE) down -v --remove-orphans
	@echo "✅ Environnement de développement totalement remis à zéro."