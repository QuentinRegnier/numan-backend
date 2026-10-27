package moderation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/gin-gonic/gin"
)

// ReplyTicketInfoHandler godoc
// @Summary      Répondre à une demande de complément d'information sur un signalement
// @Description  Permet au plaignant (auteur du signalement initial) de soumettre les éléments de clarification demandés par l'équipe de modération. L'envoi de la réponse annule le minuteur d'expiration des 7 jours, bascule immédiatement le ticket en état InProgress (1), rétablit le verrou exclusif de 48 heures au profit du modérateur initialement assigné et retire le ticket de la sous-file d'attente d'informations.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Le `callerID` extrait du contexte doit obligatoirement figurer parmi la liste des signaleurs du ticket (`ticket.ReporterIDs`), ou posséder le grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `report_id` (int64) et `message` (string non vide) dans le corps JSON (`moderation_models.ReplyTicketInfoInput`).
// @Description  - Validation rules: `report_id` > 0 et `message` nettoyé contre les injections XSS (`pkg.CleanStr`).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction et validation de l'identifiant du plaignant (`callerID`) et de son grade depuis le contexte Gin.
// @Description  2. Désérialisation et validation du corps de requête JSON (`c.ShouldBindJSON`).
// @Description  3. Récupération du ticket depuis le cache Redis L1 (`cache_service.GetModerationTicket`).
// @Description  4. Contrôle d'état : vérifie que le ticket est actuellement en attente d'information (`ReportStateWaitingForInfo = 2`).
// @Description  5. Contrôle d'habilitation : vérifie que l'appelant est bien l'un des signaleurs enregistrés (`slices.Contains(ticket.ReporterIDs, callerID)`) ou un Administrateur.
// @Description  6. Suppression du minuteur d'expiration des 7 jours dans le ZSET Redis (`moderation:expirations:waiting_info`).
// @Description  7. Retrait de la sous-file `ModerationQueueWaitingInfo`.
// @Description  8. Réactivation du verrou exclusif pour 48 heures au bénéfice du modérateur assigné (`ClaimedByModID`).
// @Description  9. Mutation d'état : passage en `ReportStateInProgress` (1), stockage de la réponse `InfoReplyMessage`, réinitialisation de `WaitingUntilMs = 0` et horodatage de l'échéance de verrou.
// @Description  10. Sauvegarde du ticket mis à jour dans Redis (`cache_service.SaveModerationTicket`).
// @Description  11. Renvoi du DTO public `moderation_models.ModerationTicketPublicView`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `moderation_models.ModerationTicketPublicView`.
// @Description  - Persistence guarantees: Mise à jour atomique du ticket en RAM Redis et suppression de l'échéance d'expiration des 7 jours.
// @Description  - Side effects: Le modérateur initialement assigné retrouve le ticket actif dans son tableau de bord avec le nouveau complément d'information.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON payload or empty reply:**
// @Description    - Trigger: Le corps JSON est malformé ou le champ `message` est vide.
// @Description    - Execution stage: Validation du corps de requête (`c.ShouldBindJSON`).
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
// @Description  - **[FORBIDDEN_ACCESS] Caller is not a reporter on this ticket:**
// @Description    - Trigger: L'utilisateur tente de répondre à une demande qui ne le concerne pas.
// @Description    - Execution stage: Contrôle des droits dans `ReplyTicketInfoService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  🟠 **404 Not Found:**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] Ticket not found:**
// @Description    - Trigger: L'identifiant `report_id` n'existe pas dans le cache.
// @Description    - Execution stage: Recherche du ticket (`cache_service.GetModerationTicket`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound`.
// @Description
// @Description  ⚠️ **409 Conflict:**
// @Description
// @Description  - **[CONFLICT] Ticket not waiting for info:**
// @Description    - Trigger: Le ticket n'est pas dans l'état `WaitingForInfo` (2).
// @Description    - Execution stage: Contrôle d'état dans `ReplyTicketInfoService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeConflict`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Redis failure or serialization error:**
// @Description    - Trigger: Échec de communication avec Redis lors du dégel du ticket ou de la réactivation du verrou.
// @Description    - Execution stage: Persistance Redis.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         moderation
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                      true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                      true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                      true "Timestamp Unix de la requête"
// @Param        input         body   moderation_models.ReplyTicketInfoInput      true "Identifiant du ticket et réponse du plaignant"
// @Success      200  {object}  moderation_models.ModerationTicketPublicView
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Ticket Not Found"
// @Failure      409  {object}  numan_error.PublicErrorResponse "Conflict (Ticket not waiting for info)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /moderation/tickets/info/reply [post]
func ReplyTicketInfoHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}
	grade := c.GetInt("user_grade")

	var input moderation_models.ReplyTicketInfoInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou réponse manquante.", err))
		return
	}

	view, errSvc := moderation_service.ReplyTicketInfoService(c.Request.Context(), callerID, grade, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, view)
}
