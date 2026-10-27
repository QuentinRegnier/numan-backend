package moderation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/gin-gonic/gin"
)

// CloseTicketHandler godoc
// @Summary      Clôturer un ticket de modération (Archivage et feedback)
// @Description  Permet à un modérateur assigné ou à un administrateur de prononcer la clôture définitive d'un ticket. L'arbitrage distingue le classement sans suite (`is_dismiss = true`, entraînant le malus abusive_report sur les faux signaleurs) de la confirmation avec sanction (`is_dismiss = false`, récompensant les signaleurs via justified_report). L'opération lève le verrou, archive le rapport dans PostgreSQL L3 (`moderation.reports`), et purge le ticket de la mémoire vive Redis.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Modérateur (`UserGradeModerator = 3`) ou Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `report_id` (int64), `is_dismiss` (boolean), et `rationale` (string non vide) dans `moderation_models.CloseTicketInput`.
// @Description  - Validation rules: `report_id` > 0 et `rationale` nettoyé.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identité du modérateur (`callerID`) depuis le contexte Gin.
// @Description  2. Validation du corps de requête JSON (`c.ShouldBindJSON`).
// @Description  3. Récupération du ticket depuis le cache Redis L1 (`cache_service.GetModerationTicket`).
// @Description  4. Contrôle d'état : vérifie que le ticket n'est pas déjà clos (`ticket.State != ReportStateClosed`).
// @Description  5. Suppression du verrou d'assignation exclusif dans Redis (`cache_service.ReleaseTicketLock`).
// @Description  6. Retrait du ticket de l'ensemble des files d'attente actives ou spécialisées Redis.
// @Description  7. Application de la décision :
// @Description     - Si `is_dismiss == true` (rejet) : émission de l'événement `TrustTriggerAbusiveReport` (-0.15) contre les signaleurs calomnieux.
// @Description     - Si `is_dismiss == false` (sanction) : émission de l'événement `TrustTriggerJustifiedReport` (+0.03) pour créditer les signaleurs citoyens.
// @Description  8. Archivage permanent du ticket dans PostgreSQL L3 (`postgres.FuncArchiveClosedReport`) au statut clos (`state = -1`) avec l'horodatage et la motivation finale.
// @Description  9. Suppression définitive du ticket de la RAM Redis (`cache_service.DeleteModerationTicket`).
// @Description  10. Renvoi du DTO de confirmation `moderation_models.CloseTicketOutput`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `moderation_models.CloseTicketOutput` contenant l'identifiant du rapport, le statut fermé et l'horodatage de clôture.
// @Description  - Persistence guarantees: Archivage ACID direct dans PostgreSQL L3 et éviction propre de la RAM Redis.
// @Description  - Side effects: Déclenchement asynchrone des réévaluations de score de réputation pour les signaleurs.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON payload or empty rationale:**
// @Description    - Trigger: Le corps JSON est malformé ou la motivation de clôture est vide.
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
// @Description  - **[FORBIDDEN_ACCESS] Insufficient privileges:**
// @Description    - Trigger: L'appelant ne dispose pas du grade requis pour clôturer un ticket.
// @Description    - Execution stage: Contrôle des privilèges d'accès.
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
// @Description  - **[CONFLICT] Ticket already closed:**
// @Description    - Trigger: Le ticket a déjà fait l'objet d'une clôture définitive.
// @Description    - Execution stage: Contrôle d'état dans `CloseTicketService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeConflict`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Database archiving failure:**
// @Description    - Trigger: Échec de l'insertion dans la table `moderation.reports` de PostgreSQL.
// @Description    - Execution stage: Archivage PostgreSQL L3.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                  true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                  true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                  true "Timestamp Unix de la requête"
// @Param        input         body   moderation_models.CloseTicketInput      true "Paramètres de clôture et motivation"
// @Success      200  {object}  moderation_models.CloseTicketOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Ticket Not Found"
// @Failure      409  {object}  numan_error.PublicErrorResponse "Conflict (Already closed)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/moderation/tickets/close [post]
func CloseTicketHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input moderation_models.CloseTicketInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou paramètres de clôture manquants.", err))
		return
	}

	out, errSvc := moderation_service.CloseTicketService(c.Request.Context(), callerID, input.IsDismiss, input.Rationale, input.ReportID)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
