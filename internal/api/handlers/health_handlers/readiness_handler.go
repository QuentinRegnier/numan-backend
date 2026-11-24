package health_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/service/health_service"
	"github.com/gin-gonic/gin"
)

// ReadinessHandler godoc
// @Summary      Sonde de disponibilité Kubernetes (Readiness Probe — /readyz)
// @Description  Évalue en temps réel l'aptitude du pod Numan à recevoir du trafic applicatif en contrôlant d'abord le drapeau de drainage gracieux (`SIGTERM`), puis en exécutant en parallèle des sondes `PING` bornées à `1.5s` sur les 3 niveaux de stockage L1/L2/L3.
// @Description  Montée directement à la racine du routeur (`GET /readyz`, hors préfixe `/api/v12`) pour tous les rôles (`monolith`, `api_node`, `ws_node`, `worker_node`, `scheduler_node`).
// @Description  Distinction stricte de criticité :
// @Description  - **Dépendances critiques (HTTP 503 `not_ready` en cas d'échec) :** `redis` (Tier 1 Core), `postgres` (Primary L3), `mongo` (L2), ainsi que `redis_pubsub` (Tier 3) **uniquement** sur les rôles temps réel `ws_node` et `monolith`.
// @Description  - **Dépendances dégradables (HTTP 200 `ready` avec statut `"degraded"`) :** `redis_objects` (Tier 2 Object Pool — bascule transparente sur MongoDB L2), `postgres_read` (Read Replica — bascule transparente sur le Primary Write), et `redis_pubsub` sur `api_node` / `worker_node` / `scheduler_node`.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Aucune (route technique interne dédiée à Kubelet `readinessProbe` et au retrait automatique des Endpoints Traefik lors d'un déploiement rolling-update).
// @Description  - Required permissions or roles: Aucun.
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware (exemptée de JWTMiddleware, HMACMiddleware, RateLimiter et AdminGuard).
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: Aucun corps JSON, aucun paramètre de chemin ni paramètre de query string.
// @Description  - Validation rules: Aucun paramètre métier n'est accepté conformément à la règle API sans paramètres d'URL.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Réception de la requête `GET /readyz` par le handler fin `health_handlers.ReadinessHandler` (déléguant à `NewReadinessHandler(health_service.DefaultReadinessChecker())`).
// @Description  2. Vérification atomique du drainage gracieux (`health_service.IsShuttingDown`) : si un signal `SIGTERM`/`SIGINT` a été reçu, retour immédiat HTTP 503 (`status: "shutting_down"`) sans interroger les bases pour retirer instantanément le pod du Load Balancer.
// @Description  3. Création d'un contexte borné à `1.5s` et lancement en parallèle (`sync.WaitGroup`) des sondes via les couches DDD appropriées : `cache_service` (`PingRedisCore`, `PingRedisObjects`, `PingRedisPubSub`), `repository/postgres` (`FuncPingWrite`, `FuncPingRead`), et `repository/mongo` (`FuncPingMongo`).
// @Description  4. Agrégation de la carte `checks` (`ok`, `degraded`, `error`, `skipped`), enrichissement avec l'identité du nœud (`role`, `datacenter`, `node_id`) et calcul du booléen global `ready`.
// @Description  5. Journalisation structurée en `WARN` uniquement si le pod est non-prêt (`!ready`) afin d'éviter tout bruit en régime nominal, puis renvoi HTTP 200 OK (`status: "ready"`) ou HTTP 503 Service Unavailable (`status: "not_ready"`).
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `health_models.ReadinessOutput` (`status: "ready"`, carte détaillée `checks`, identité du nœud `role`/`datacenter`/`node_id`).
// @Description  - Persistence guarantees: Sondes `PING` strictement en lecture seule ; aucune mutation L1/L2/L3.
// @Description  - Side effects: Aucun en régime nominal.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Unexpected runtime panic recovery:**
// @Description    - Trigger: Panique système imprévue interceptée par `CustomRecoveryMiddleware`.
// @Description    - Execution stage: Exécution du handler.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Description
// @Description  🟡 **503 Service Unavailable:**
// @Description
// @Description  - **[SHUTTING_DOWN] Graceful pod draining in progress:**
// @Description    - Trigger: Le pod a reçu `SIGTERM`/`SIGINT` et `health_service.SetShuttingDown(true)` est actif.
// @Description    - Execution stage: Contrôle préalable dans `health_service.CheckReadiness`.
// @Description    - Response: `health_models.ReadinessOutput` (`status: "shutting_down"`).
// @Description
// @Description  - **[NOT_READY] Critical datastore dependency unavailable or timed out (>1.5s):**
// @Description    - Trigger: Échec ou expiration (`>1.5s`) de la sonde sur `redis` (Core), `postgres` (Write Primary), `mongo`, ou `redis_pubsub` (si `role` est `ws_node` ou `monolith`).
// @Description    - Execution stage: Sondes parallèles dans `health_service.CheckReadiness`.
// @Description    - Response: `health_models.ReadinessOutput` (`status: "not_ready"`, détail par composant dans `checks`).
// @Tags         health
// @Produce      json
// @Success      200  {object}  health_models.ReadinessOutput "Pod prêt à recevoir du trafic (toutes les dépendances critiques sont saines)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Failure      503  {object}  health_models.ReadinessOutput "Service Unavailable (Pod en drainage SIGTERM ou dépendance critique indisponible)"
// @Router       /readyz [get]
func ReadinessHandler(c *gin.Context) {
	NewReadinessHandler(health_service.DefaultReadinessChecker())(c)
}

// NewReadinessHandler construit un handler Gin /readyz à partir d'un ReadinessChecker injectable (tests unitaires).
func NewReadinessHandler(checker health_service.ReadinessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		out, ready := health_service.CheckReadiness(c.Request.Context(), checker)
		if !ready {
			c.JSON(http.StatusServiceUnavailable, out)
			return
		}
		c.JSON(http.StatusOK, out)
	}
}
