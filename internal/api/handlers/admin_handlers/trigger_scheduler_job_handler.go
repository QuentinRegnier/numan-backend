package admin_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/admin_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
)

// TriggerSchedulerJobHandler godoc
// @Summary      Déclencher manuellement un job planifié du Scheduler (Administrateur uniquement)
// @Description  Permet à un Administrateur de lancer immédiatement un job du catalogue de l'Activity-Aware Scheduler (par exemple `pg_expired_sessions_purge`, `most_cache_surgical_purify` ou `dlq_audit_and_replay`). En mode `monolith`, le job s'exécute directement et retourne HTTP 200 (`status: "executed"`). En topologie distribuée (`api_node` -> `scheduler_node`), une commande de déclenchement distant est déposée dans Redis Core et retourne HTTP 202 Accepted (`status: "queued_remote_trigger"`). L'exécution est refusée en conflit (409) si le Scheduler est en pause ou en régime `OVERLOAD`, sauf si `force: true` est explicitement passé dans le corps JSON.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Le compte appelant doit posséder strictement le grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `job_name` (chaîne snake_case `[a-z0-9_]{2,128}`) dans `admin_models.TriggerSchedulerJobInput`.
// @Description  - Optional fields: `force` (booléen, défaut `false` : force le déclenchement malgré une pause ou un régime `OVERLOAD`).
// @Description  - Validation rules: Corps JSON obligatoire ; aucun paramètre d'URL ou de query string n'est utilisé.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identifiant administrateur (`callerID`) et de son grade depuis le contexte Gin.
// @Description  2. Vérification stricte du grade Administrateur (rejet 403 sinon).
// @Description  3. Désérialisation du corps JSON (`c.ShouldBindJSON`) dans `admin_models.TriggerSchedulerJobInput`.
// @Description  4. Délégation à `admin_service.TriggerSchedulerJobService` (validation métier + déclenchement local ou dépôt en file distante Redis Core via `scheduler_service.TriggerJobGlobal`).
// @Description  5. Retour HTTP 202 Accepted si mis en file distante (`out.Queued == true`), sinon HTTP 200 OK avec le statut final du job.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK (exécution locale) ou 202 Accepted (déclenchement distant vers `scheduler_node`).
// @Description  - Response body: JSON `admin_models.SchedulerActionOutput`.
// @Description  - Persistence guarantees: Statut et checkpoint persistés dans Redis Core (`scheduler:job:<job_name>`).
// @Description  - Side effects: Exécution du job cible sous verrou distribué (`LocalDC` ou `GlobalSQL`) et audit log structuré.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Missing or malformed job_name:**
// @Description    - Trigger: Corps JSON manquant/malformé ou `job_name` ne respectant pas `^[a-z0-9_]{2,128}$`.
// @Description    - Execution stage: Désérialisation JSON (`c.ShouldBindJSON`) ou validation métier (`input.Validate()`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED] Missing or expired JWT:**
// @Description    - Trigger: Jeton d'authentification absent, expiré ou corrompu.
// @Description    - Execution stage: Extraction du contexte (`pkg.GetUserIDFromContext`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized`.
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Administrator grade required:**
// @Description    - Trigger: L'utilisateur ne possède pas le grade Administrateur (4).
// @Description    - Execution stage: Contrôle des privilèges administratifs.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  🟠 **404 Not Found:**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] Unknown scheduler job:**
// @Description    - Trigger: `job_name` ne correspond à aucun job enregistré dans le catalogue du Scheduler local.
// @Description    - Execution stage: Recherche dans `scheduler_service.Engine.TriggerJob`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound`.
// @Description
// @Description  🟡 **409 Conflict:**
// @Description
// @Description  - **[RESOURCE_CONFLICT] Scheduler paused, in OVERLOAD, or busy:**
// @Description    - Trigger: Le Scheduler est en pause ou en régime `OVERLOAD` sans `force: true`, ou un autre job est déjà en cours d'exécution.
// @Description    - Execution stage: Vérification des préconditions dans `scheduler_service.TriggerJobGlobal`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeConflict`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[CACHE_ERROR] Redis remote trigger enqueue failure:**
// @Description    - Trigger: Indisponibilité de Redis Core lors de l'enregistrement de la commande distante.
// @Description    - Execution stage: Écriture dans `cache_service.EnqueueSchedulerRemoteTrigger`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeCacheError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                  true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                  true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                  true "Timestamp Unix de la requête"
// @Param        input         body   admin_models.TriggerSchedulerJobInput   true "Nom du job et option force"
// @Success      200  {object}  admin_models.SchedulerActionOutput "Executed synchronously on local scheduler"
// @Success      202  {object}  admin_models.SchedulerActionOutput "Queued for remote scheduler_node execution"
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin only)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Job Not Found"
// @Failure      409  {object}  numan_error.PublicErrorResponse "Conflict (Paused / Overload / Busy)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/scheduler/jobs/trigger [post]
func TriggerSchedulerJobHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}
	grade := c.GetInt("user_grade")
	if grade != variables.UserGradeAdmin && grade != variables.MemberRoleAdmin {
		numan_error.RespondWithError(c, numan_error.NewForbidden(numan_error.CodeForbidden, "Privilèges d'administrateur requis.", nil))
		return
	}

	if c.Request != nil && c.Request.URL != nil && c.Request.URL.RawQuery != "" {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres d'URL (query string) sont interdits sur cette route.", nil))
		return
	}

	var input admin_models.TriggerSchedulerJobInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide.", err))
		return
	}

	out, errSvc := admin_service.TriggerSchedulerJobService(c.Request.Context(), callerID, grade, &input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	if out.Queued {
		c.JSON(http.StatusAccepted, out)
		return
	}
	c.JSON(http.StatusOK, out)
}
