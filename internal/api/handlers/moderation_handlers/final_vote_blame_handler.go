package moderation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/gin-gonic/gin"
)

// FinalVoteBlameHandler godoc
// @Summary      Arbitrage final d'une contradiction de blâme (Super-Modérateur ou Administrateur)
// @Description  Permet à un Super-Modérateur indépendant (possédant une fiabilité $T_{\text{mod}} \ge 0.90$) ou à un Administrateur (Grade 4) de trancher définitivement une situation de désaccord entre le Modérateur A et le Modérateur B. L'arbitre statue entre la confirmation du blâme (`confirm_blame`) ou son rejet (`reject_blame`). Cette décision souveraine clôture le ticket et l'archive en base PostgreSQL.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Compte Administrateur (`UserGradeAdmin = 4`), ou Super-Modérateur actif possédant un score de fiabilité $T_{\text{mod}} \ge 0.90$ (`SuperModeratorTrustThreshold`).
// @Description  - Independence rule: L'arbitre ne peut être ni le Modérateur A (`ProposerModID`), ni le Modérateur B (ayant émis l'objection).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `report_id` (int64), `decision` (`"confirm_blame"` ou `"reject_blame"`), et `rationale` (string) dans `moderation_models.FinalVoteBlameInput`.
// @Description  - Validation rules: `decision` doit strictement valoir `"confirm_blame"` ou `"reject_blame"`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identité de l'arbitre (`callerID`) et de son grade depuis le contexte Gin.
// @Description  2. Validation de la charge utile JSON (`c.ShouldBindJSON`).
// @Description  3. Contrôle des privilèges Super-Modérateur : si l'appelant n'est pas Administrateur, interrogation du cache $T_{\text{mod}}$ (`cache_service.GetOrInitModeratorStats`) pour valider $T_{\text{mod}} \ge 0.90$.
// @Description  4. Chargement du ticket depuis Redis L1 (`cache_service.GetModerationTicket`).
// @Description  5. Contrôle d'état : vérifie que le ticket est en attente d'arbitrage (`ReportStatePendingFinalVote = 5` ou `ReportStateEscalated = 4`).
// @Description  6. Retrait du ticket de la file d'attente (`ModerationQueueFinalVote` ou `ModerationQueueAdmin`).
// @Description  7. Application de la décision :
// @Description     - En cas de confirmation (`confirm_blame`) : application du blâme sur le profil cible, pénalité réputationnelle `TrustTriggerMinorInfraction` (-0.25), masquage préventif du contenu et vérification du seuil de 3 blâmes.
// @Description     - En cas de rejet (`reject_blame`) : le blâme est annulé, aucune sanction n'est appliquée à la cible.
// @Description  8. Clôture définitive du ticket (`ReportStateClosed = -1`), enregistrement du motif d'arbitrage et archivage PostgreSQL L3 (`postgres.FuncArchiveClosedReport`).
// @Description  9. Suppression du ticket de la mémoire active Redis.
// @Description  10. Renvoi du DTO public `moderation_models.ModerationTicketPublicView`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `moderation_models.ModerationTicketPublicView`.
// @Description  - Persistence guarantees: Archivage PostgreSQL immuable et éviction de la file active Redis.
// @Description  - Side effects: Mise à jour du score de réputation de la cible si blâmée, et clôture définitive du litige.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON payload or invalid decision:**
// @Description    - Trigger: Décision différente de `"confirm_blame"` / `"reject_blame"`, ou justification vide.
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
// @Description  - **[FORBIDDEN_ACCESS] Insufficient reliability or admin grade required:**
// @Description    - Trigger: L'appelant n'est pas Administrateur et possède une fiabilité $T_{\text{mod}} < 0.90$, ou a participé à l'instruction initiale.
// @Description    - Execution stage: Contrôle des habilitations Super-Modérateur dans `ResolveFinalVoteService`.
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
// @Description  - **[CONFLICT] Ticket not in final vote state:**
// @Description    - Trigger: Le ticket n'est pas en état de vote final (`ReportStatePendingFinalVote = 5`).
// @Description    - Execution stage: Contrôle d'état dans `ResolveFinalVoteService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeConflict`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Database archiving failure:**
// @Description    - Trigger: Échec de l'insertion dans la table `moderation.reports` de PostgreSQL lors de l'archivage.
// @Description    - Execution stage: Persistance de la clôture légale.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                    true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                    true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                    true "Timestamp Unix de la requête"
// @Param        input         body   moderation_models.FinalVoteBlameInput      true "Décision finale (confirm_blame ou reject_blame) et motivation"
// @Success      200  {object}  moderation_models.ModerationTicketPublicView
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Ticket Not Found"
// @Failure      409  {object}  numan_error.PublicErrorResponse "Conflict (Wrong state)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/moderation/tickets/blame/final-vote [post]
func FinalVoteBlameHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}
	grade := c.GetInt("user_grade")

	var input moderation_models.FinalVoteBlameInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou décision manquante (confirm_blame/reject_blame).", err))
		return
	}

	view, errSvc := moderation_service.ResolveFinalVoteService(c.Request.Context(), callerID, grade, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, view)
}
