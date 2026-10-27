package moderation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/gin-gonic/gin"
)

// ReleaseTicketHandler godoc
// @Summary      Libérer un ticket de modération (Retour en Pending)
// @Description  Permet au modérateur assigné ou à un administrateur de libérer volontairement un ticket en cours de traitement. L'opération lève le verrou exclusif Redis, réinitialise l'état à Pending (0), efface l'assignation du modérateur et réindexe le ticket dans la file de modération active avec son score de priorité initial.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Le `callerID` doit correspondre au modérateur ayant pris en charge le ticket (`ClaimedByModID`), ou posséder le grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `report_id` (int64) dans le corps JSON (`moderation_models.ReleaseTicketInput`).
// @Description  - Validation rules: `report_id` strictement positif (`binding:"required,gt=0"`).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction et validation de l'identifiant du modérateur (`callerID`) et de son grade depuis le contexte Gin.
// @Description  2. Désérialisation et validation du corps de requête JSON (`c.ShouldBindJSON`).
// @Description  3. Récupération du ticket depuis le cache Redis L1 (`cache_service.GetModerationTicket`).
// @Description  4. Contrôle d'état : vérifie que le ticket est actuellement en cours de traitement (`ReportStateInProgress = 1`).
// @Description  5. Contrôle d'habilitation : vérifie que l'appelant est soit le modérateur assigné (`ClaimedByModID == callerID`), soit un Administrateur.
// @Description  6. Suppression du verrou distribué exclusif dans Redis (`cache_service.ReleaseTicketLock`).
// @Description  7. Mutation d'état : passage en `ReportStatePending` (0), réinitialisation de `ClaimedByModID = 0` et de `LockExpiresAtMs = 0`.
// @Description  8. Réindexation atomique du ticket dans la file active (`cache_service.IndexTicketInModerationQueue(ModerationQueueActive, reportID, priorityScore)`).
// @Description  9. Sauvegarde du ticket mis à jour dans Redis (`cache_service.SaveModerationTicket`).
// @Description  10. Renvoi du DTO public `moderation_models.ModerationTicketPublicView`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `moderation_models.ModerationTicketPublicView`.
// @Description  - Persistence guarantees: Suppression du verrou Redis, mise à jour du Hash du ticket et réinsertion ordonnée dans le ZSET de la file active.
// @Description  - Side effects: Le ticket redevient immédiatement disponible pour réclamation par tout autre modérateur de la plateforme.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON payload or missing report_id:**
// @Description    - Trigger: Le corps JSON est malformé ou `report_id` est manquant / invalide.
// @Description    - Execution stage: Validation du corps de requête (`c.ShouldBindJSON`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED] Missing or expired JWT:**
// @Description    - Trigger: Le jeton d'authentification est absent, invalide ou expiré.
// @Description    - Execution stage: Extraction du contexte (`pkg.GetUserIDFromContext`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized`.
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Caller is not the assigned moderator:**
// @Description    - Trigger: L'appelant n'est pas le modérateur assigné au ticket et ne possède pas le grade Administrateur.
// @Description    - Execution stage: Contrôle des privilèges dans `ReleaseTicketService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  🟠 **404 Not Found:**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] Ticket not found:**
// @Description    - Trigger: L'identifiant `report_id` est introuvable dans le cache de modération.
// @Description    - Execution stage: Recherche du ticket (`cache_service.GetModerationTicket`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound`.
// @Description
// @Description  ⚠️ **409 Conflict:**
// @Description
// @Description  - **[CONFLICT] Ticket not in progress:**
// @Description    - Trigger: Le ticket n'est pas dans l'état `InProgress` (1) requis pour être libéré.
// @Description    - Execution stage: Vérification de l'état du ticket dans `ReleaseTicketService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeConflict`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Redis failure or serialization error:**
// @Description    - Trigger: Échec de communication avec Redis lors de la suppression du verrou ou de la mise à jour du ticket.
// @Description    - Execution stage: Opérations de persistance Redis.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                    true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                    true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                    true "Timestamp Unix de la requête"
// @Param        input         body   moderation_models.ReleaseTicketInput      true "Identifiant du ticket de modération à libérer"
// @Success      200  {object}  moderation_models.ModerationTicketPublicView
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Ticket Not Found"
// @Failure      409  {object}  numan_error.PublicErrorResponse "Conflict (Ticket not in progress)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/moderation/tickets/release [post]
func ReleaseTicketHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}
	grade := c.GetInt("user_grade")

	var input moderation_models.ReleaseTicketInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou identifiant manquant.", err))
		return
	}

	view, errSvc := moderation_service.ReleaseTicketService(c.Request.Context(), callerID, grade, input.ReportID)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, view)
}
