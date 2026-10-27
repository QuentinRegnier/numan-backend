package moderation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/gin-gonic/gin"
)

// ClaimTicketHandler godoc
// @Summary      Réclamer un ticket de modération (Verrou exclusif 48h)
// @Description  Permet à un modérateur ou administrateur de prendre en charge un ticket en attente dans la file de modération active. Pose un verrou distribué exclusif (TTL 48h), retire le ticket de la file active pour éviter tout traitement concurrentiel et fait transiter son état de Pending (0) à InProgress (1).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Le `userID` extrait du contexte doit correspondre à un compte actif possédant au minimum le grade Modérateur (`UserGradeModerator = 3`) ou Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `report_id` (int64) dans le corps JSON (`moderation_models.ClaimTicketInput`).
// @Description  - Validation rules: `report_id` strictement positif (`binding:"required,gt=0"`).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction et validation de l'identifiant du modérateur (`callerID`) depuis le contexte Gin sécurisé (`pkg.GetUserIDFromContext`).
// @Description  2. Désérialisation et validation du corps de requête JSON (`c.ShouldBindJSON`).
// @Description  3. Récupération du ticket dans le cache de modération L1 (`cache_service.GetModerationTicket`).
// @Description  4. Contrôle de l'état du ticket : vérifie que l'état actuel est strictement `ReportStatePending` (0).
// @Description  5. Acquisition atomique du verrou distribué exclusif (`cache_service.AcquireTicketLock`) d'une durée de 48 heures (`variables.ModerationLockTTL`) dans Redis (`moderation:locks:<report_id>`) et enregistrement dans le ZSET des expirations.
// @Description  6. Retrait du ticket de la file active (`cache_service.RemoveTicketFromModerationQueue(ModerationQueueActive, reportID)`).
// @Description  7. Mutation d'état : passage en `ReportStateInProgress` (1), affectation de `ClaimedByModID = callerID` et calcul de l'échéance `LockExpiresAtMs`.
// @Description  8. Sauvegarde du ticket mis à jour dans le cache Redis (`cache_service.SaveModerationTicket`).
// @Description  9. Conversion en DTO sécurisé (`ToPublicTicketView(ticket, false)`) et renvoi de la réponse JSON.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `moderation_models.ModerationTicketPublicView`.
// @Description  - Persistence guarantees: Mutation atomique en mémoire Redis (Hash ticket, clé de verrou et ZSET d'expiration).
// @Description  - Side effects: Démarrage du compte à rebours d'expiration de 48h surveillé par le worker d'arrière-plan (`moderation_worker.go`).
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON payload or missing report_id:**
// @Description    - Trigger: Le corps JSON est malformé ou `report_id` est manquant / négatif.
// @Description    - Execution stage: Validation du corps de requête (`c.ShouldBindJSON`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED] Missing or expired JWT:**
// @Description    - Trigger: Le jeton d'authentification est absent, invalide ou expiré, ou `callerID <= 0`.
// @Description    - Execution stage: Extraction du contexte (`pkg.GetUserIDFromContext`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized`.
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Insufficient privileges:**
// @Description    - Trigger: L'utilisateur ne dispose pas du grade requis pour modérer.
// @Description    - Execution stage: Contrôle des privilèges d'accès.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  🟠 **404 Not Found:**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] Moderation ticket not found:**
// @Description    - Trigger: L'identifiant `report_id` spécifié n'existe pas dans le cache Redis de modération.
// @Description    - Execution stage: Recherche du ticket (`cache_service.GetModerationTicket`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound`.
// @Description
// @Description  ⚠️ **409 Conflict:**
// @Description
// @Description  - **[CONFLICT] Ticket not in pending state or already locked:**
// @Description    - Trigger: Le ticket n'est pas en état `Pending` (0), ou un autre modérateur a déjà acquis le verrou exclusif.
// @Description    - Execution stage: Contrôle d'état ou acquisition du verrou (`cache_service.AcquireTicketLock`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeConflict`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Redis failure or serialization error:**
// @Description    - Trigger: Panne de communication avec le cluster Redis lors de la persistance ou de l'acquisition du verrou.
// @Description    - Execution stage: Sauvegarde du ticket ou manipulation des files Redis.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                  true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                  true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                  true "Timestamp Unix de la requête"
// @Param        input         body   moderation_models.ClaimTicketInput      true "Identifiant du ticket de modération à réclamer"
// @Success      200  {object}  moderation_models.ModerationTicketPublicView
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Ticket Not Found"
// @Failure      409  {object}  numan_error.PublicErrorResponse "Conflict (Already claimed or wrong state)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/moderation/tickets/claim [post]
func ClaimTicketHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input moderation_models.ClaimTicketInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou identifiant de ticket manquant.", err))
		return
	}

	view, errSvc := moderation_service.ClaimTicketService(c.Request.Context(), callerID, input.ReportID)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, view)
}
