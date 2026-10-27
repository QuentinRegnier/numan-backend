package moderation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/gin-gonic/gin"
)

// ProposeBlameHandler godoc
// @Summary      Proposer un blâme sur un contenu (Procédure Four-Eyes)
// @Description  Permet au modérateur assigné (Modérateur A) de proposer une sanction avec motivation détaillée. Bascule le ticket en état PendingPeerReview (3), libère le verrou exclusif et indexe le ticket dans la file de revue croisée (peer_review) pour évaluation par un second modérateur indépendant (Modérateur B). L'identité du Modérateur A est conservée en interne sur le serveur mais masquée dans la réponse API pour garantir l'impartialité.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Le `callerID` doit correspondre au modérateur ayant pris en charge le ticket (`ClaimedByModID`), ou posséder le grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `report_id` (int64) et `motivation` (string non vide) dans le corps JSON (`moderation_models.ProposeBlameInput`).
// @Description  - Validation rules: `report_id` > 0 et `motivation` nettoyée contre les injections (`pkg.CleanStr`).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction et validation de l'identifiant du modérateur (`callerID`) et de son grade depuis le contexte Gin.
// @Description  2. Désérialisation et validation du corps de requête JSON (`c.ShouldBindJSON`).
// @Description  3. Récupération du ticket dans le cache Redis L1 (`cache_service.GetModerationTicket`).
// @Description  4. Contrôle d'état : vérifie que le ticket est actuellement en cours de traitement (`ReportStateInProgress = 1`).
// @Description  5. Contrôle d'habilitation : vérifie que l'appelant est le modérateur assigné ou un Administrateur.
// @Description  6. Levée du verrou exclusif (`cache_service.ReleaseTicketLock`) pour permettre la prise en charge future par un pair.
// @Description  7. Mutation d'état : passage en `ReportStatePendingPeerReview` (3), enregistrement de `ProposerModID = callerID` en interne et stockage de la motivation nettoyée.
// @Description  8. Réindexation atomique du ticket dans la sous-file de validation croisée (`ModerationQueuePeerReview`).
// @Description  9. Sauvegarde du ticket mis à jour dans Redis (`cache_service.SaveModerationTicket`).
// @Description  10. Conversion en DTO sécurisé (`ToPublicTicketView(ticket, false)`) masquant l'identifiant du modérateur A pour les non-administrateurs.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `moderation_models.ModerationTicketPublicView`.
// @Description  - Persistence guarantees: Mutation persistée dans Redis (mise à jour du Hash ticket et insertion dans le ZSET peer_review).
// @Description  - Side effects: Le ticket apparaît immédiatement dans la file de revue par les pairs pour les autres modérateurs.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON payload or missing motivation:**
// @Description    - Trigger: Le corps JSON est malformé ou la motivation est vide.
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
// @Description    - Trigger: L'appelant n'est pas le modérateur assigné à ce ticket.
// @Description    - Execution stage: Contrôle des privilèges dans `ProposeBlameService`.
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
// @Description    - Execution stage: Contrôle d'état dans `ProposeBlameService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeConflict`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Redis failure or serialization error:**
// @Description    - Trigger: Panne de Redis lors de l'enregistrement ou de la réindexation.
// @Description    - Execution stage: Sauvegarde dans Redis.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                  true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                  true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                  true "Timestamp Unix de la requête"
// @Param        input         body   moderation_models.ProposeBlameInput      true "Identifiant du ticket et motivation du blâme"
// @Success      200  {object}  moderation_models.ModerationTicketPublicView
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Ticket Not Found"
// @Failure      409  {object}  numan_error.PublicErrorResponse "Conflict (Ticket not in progress)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/moderation/tickets/blame/propose [post]
func ProposeBlameHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}
	grade := c.GetInt("user_grade")

	var input moderation_models.ProposeBlameInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou motivation insuffisante.", err))
		return
	}

	view, errSvc := moderation_service.ProposeBlameService(c.Request.Context(), callerID, grade, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, view)
}
