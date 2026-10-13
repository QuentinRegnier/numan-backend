# ==============================================================================
# MAKEFILE NUMAN BACKEND - DÉVELOPPEMENT LOCAL
# ==============================================================================

# Variables pour centraliser l'appel à Docker Compose avec les bons environnements
ENV_ARGS = --env-file k8s/infrastructure.env --env-file k8s/01-config/secrets.env
COMPOSE  = docker compose $(ENV_ARGS) -f docker-compose.dev.yml

.PHONY: help docs dev down logs test clean

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
# 2. OUTILS ET NETTOYAGE
# ------------------------------------------------------------------------------

test: ## Lance tous les tests unitaires du projet Go
	@echo "🧪 Exécution des tests..."
	go test -v ./...

clean: down ## ⚠️ Arrête et DÉTRUIT les bases de données locales (Hard Reset)
	@echo "🧹 Suppression des conteneurs, réseaux et volumes locaux..."
	$(COMPOSE) down -v --remove-orphans
	@echo "✅ Environnement de développement totalement remis à zéro."