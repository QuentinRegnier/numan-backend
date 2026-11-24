package health_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/service/health_service"
	"github.com/gin-gonic/gin"
)

// LivenessHandler godoc
// @Summary      Sonde de vivacité Kubernetes (Liveness Probe — /healthz)
// @Description  Vérifie que le processus Go du pod Numan est vivant et que la boucle HTTP Gin répond normalement.
// @Description  Montée directement à la racine du routeur (`GET /healthz`, hors préfixe `/api/v12`) afin de rester active sur l'ensemble des rôles de déploiement (`monolith`, `api_node`, `ws_node`, `worker_node`, `scheduler_node`) sans authentification ni rate-limit.
// @Description  Cette sonde est volontairement isolée des bases de données externes (Redis, MongoDB, PostgreSQL) et reste en HTTP 200 OK pendant la phase de drainage gracieux (`SIGTERM`), afin d'empêcher Kubelet de redémarrer brutalement (`SIGKILL`) un pod sain lors d'un basculement temporaire de base ou pendant le vidage des workers Write-Behind / checkpoints du Scheduler (seule la sonde `/readyz` bascule en HTTP 503).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Aucune (route technique interne dédiée à Kubelet `livenessProbe` et aux vérifications d'infrastructure locales ; jamais exposée publiquement sur Internet).
// @Description  - Required permissions or roles: Aucun.
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware (exemptée de JWTMiddleware, HMACMiddleware, RateLimiter et AdminGuard).
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: Aucun corps JSON, aucun paramètre de chemin ni paramètre de query string.
// @Description  - Validation rules: Aucun paramètre métier n'est accepté conformément à la règle API sans paramètres d'URL.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Réception de la requête `GET /healthz` par le handler fin `health_handlers.LivenessHandler`.
// @Description  2. Délégation immédiate au service métier `health_service.CheckLiveness(ctx)` qui assemble le DTO `health_models.LivenessOutput` sans aucun appel réseau ni E/S disque.
// @Description  3. Enrichissement conditionnel depuis l'identité du nœud initialisée au démarrage (`health_service.SetNodeIdentity`) : `role` (`SERVER_ROLE`), `datacenter` (`DATACENTER_ID`) et `node_id` (identifiant Snowflake 10 bits).
// @Description  4. Retour immédiat en HTTP 200 OK (`status: "alive"`).
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `health_models.LivenessOutput` contenant `status` (`"alive"`), ainsi que les champs optionnels `role`, `datacenter` et `node_id` lorsqu'ils sont configurés.
// @Description  - Persistence guarantees: Lecture seule en mémoire vive (`atomic` / snapshot d'identité), latence sub-milliseconde, zéro appel L1/L2/L3.
// @Description  - Side effects: Aucun (exemptée de logs verbeux afin d'éviter tout bruit sur les sondes haute fréquence Kubelet).
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Unexpected runtime panic recovery:**
// @Description    - Trigger: Panique système imprévue dans le processus Go interceptée par `CustomRecoveryMiddleware`.
// @Description    - Execution stage: Exécution du handler ou sérialisation de la réponse JSON.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         health
// @Produce      json
// @Success      200  {object}  health_models.LivenessOutput "Processus Go vivant et opérationnel"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /healthz [get]
func LivenessHandler(c *gin.Context) {
	out := health_service.CheckLiveness(c.Request.Context())
	c.JSON(http.StatusOK, out)
}
