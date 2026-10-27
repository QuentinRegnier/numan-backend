package moderation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/gin-gonic/gin"
)

// EscalateTicketHandler godoc
// @Summary      Escalader un ticket vers la file Administrateur (Demande de Hard Ban ou Clarification CGU)
// @Description  Permet au modérateur assigné de transmettre un ticket critique aux Administrateurs (Grade 4). Utilisé pour solliciter un bannissement définitif immédiat (CSAM, terrorisme, menaces létales) ou pour demander un arbitrage juridique sur une zone grise des CGU. Le ticket bascule en état Escalated (4), le verrou modérateur est levé et le ticket est enfilé dans la file prioritaire `ModerationQueueAdmin`.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Le `callerID` doit correspondre au modérateur assigné (`ClaimedByModID`), ou posséder le grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `report_id` (int64), `escalation_type` (`"hard_ban_request"` ou `"cgu_gray_area"`), et `reason` (string non vide) dans `moderation_models.EscalateTicketInput`.
// @Description  - Validation rules: `escalation_type` restreint aux valeurs autorisées et `reason` nettoyé.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identité du modérateur (`callerID`) et de son grade depuis le contexte Gin.
// @Description  2. Validation de la charge utile JSON (`c.ShouldBindJSON`).
// @Description  3. Récupération du ticket depuis le cache Redis L1 (`cache_service.GetModerationTicket`).
// @Description  4. Contrôle d'état : vérifie que le ticket est en cours de traitement (`ReportStateInProgress = 1`).
// @Description  5. Contrôle d'habilitation : vérifie que l'appelant est le modérateur assigné ou un Administrateur.
// @Description  6. Levée du verrou exclusif (`cache_service.ReleaseTicketLock`).
// @Description  7. Mutation d'état : passage en `ReportStateEscalated` (4), enregistrement du type d'escalade (`hard_ban_request` ou `cgu_gray_area`) et de la justification.
// @Description  8. Réindexation atomique du ticket dans la file des administrateurs (`ModerationQueueAdmin`).
// @Description  9. Sauvegarde du ticket mis à jour dans Redis (`cache_service.SaveModerationTicket`).
// @Description  10. Renvoi du DTO public `moderation_models.ModerationTicketPublicView`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `moderation_models.ModerationTicketPublicView`.
// @Description  - Persistence guarantees: Mise à jour du Hash ticket en RAM Redis et indexation ordonnée dans la file administrateur.
// @Description  - Side effects: Le ticket est immédiatement visible par l'équipe administrative pour décision souveraine.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON payload or invalid escalation type:**
// @Description    - Trigger: Le type d'escalade n'est pas `"hard_ban_request"` ou `"cgu_gray_area"`, ou la justification est absente.
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
// @Description  - **[FORBIDDEN_ACCESS] Caller is not the assigned moderator:**
// @Description    - Trigger: L'appelant n'est pas le modérateur assigné au ticket et n'est pas Administrateur.
// @Description    - Execution stage: Contrôle des privilèges dans `EscalateTicketService`.
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
// @Description  - **[CONFLICT] Ticket not in progress:**
// @Description    - Trigger: Le ticket n'est pas dans l'état `InProgress` (1).
// @Description    - Execution stage: Contrôle d'état dans `EscalateTicketService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeConflict`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Redis failure or serialization error:**
// @Description    - Trigger: Échec de communication avec Redis lors de l'enregistrement de l'escalade.
// @Description    - Execution stage: Persistance Redis.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                    true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                    true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                    true "Timestamp Unix de la requête"
// @Param        input         body   moderation_models.EscalateTicketInput       true "Type d'escalade (hard_ban_request ou cgu_gray_area) et motif"
// @Success      200  {object}  moderation_models.ModerationTicketPublicView
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Ticket Not Found"
// @Failure      409  {object}  numan_error.PublicErrorResponse "Conflict (Ticket not in progress)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/moderation/tickets/escalate [post]
func EscalateTicketHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}
	grade := c.GetInt("user_grade")

	var input moderation_models.EscalateTicketInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou type d'escalade incorrect.", err))
		return
	}

	view, errSvc := moderation_service.EscalateTicketService(c.Request.Context(), callerID, grade, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, view)
}
