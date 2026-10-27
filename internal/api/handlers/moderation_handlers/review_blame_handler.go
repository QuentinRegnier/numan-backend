package moderation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/gin-gonic/gin"
)

// ReviewBlameHandler godoc
// @Summary      Examiner une proposition de blâme par un pair modérateur (Four-Eyes)
// @Description  Permet à un second modérateur indépendant (Modérateur B) de valider ou rejeter la proposition de blâme formulée par le Modérateur A. En cas de consensus positif (`validate`), le blâme est acté, le contenu est masqué, le signalement justifié est récompensé, et le ticket est archivé. En cas d'opposition (`oppose`), une contradiction est ouverte : le ticket est routé soit vers les Super-Modérateurs (`FinalVote`), soit vers les Administrateurs selon l'état de l'interrupteur de délégation.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Modérateur (`UserGradeModerator = 3`) ou Administrateur (`UserGradeAdmin = 4`).
// @Description  - Strict constraint (Four-Eyes): L'appelant (Modérateur B) doit obligatoirement être différent du modérateur proposant (Modérateur A : `callerID != ticket.ProposerModID`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `report_id` (int64), `decision` (`"validate"` ou `"oppose"`), et `rationale` (string) dans `moderation_models.ReviewBlameInput`.
// @Description  - Validation rules: `decision` doit strictement valoir `"validate"` ou `"oppose"`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identité du Modérateur B (`callerID`) et de son grade depuis le contexte Gin.
// @Description  2. Validation de la charge utile JSON (`c.ShouldBindJSON`).
// @Description  3. Chargement du ticket depuis Redis L1 (`cache_service.GetModerationTicket`).
// @Description  4. Contrôle d'état : vérifie que le ticket est en `ReportStatePendingPeerReview` (3).
// @Description  5. Application de la règle des 4 yeux : vérifie que `callerID != ticket.ProposerModID` (rejet 403 en cas d'auto-validation).
// @Description  6. Retrait du ticket de la sous-file `ModerationQueuePeerReview`.
// @Description  7. Traitement selon la décision :
// @Description     - **Si `validate` (Consensus)** :
// @Description       a. Enregistrement du blâme dans les statistiques de la cible (`TargetOwnerID`) et incrémentation de son compteur de blâmes.
// @Description       b. Émission d'un événement `TrustTriggerMinorInfraction` (-0.25) contre l'auteur du contenu répréhensible.
// @Description       c. Masquage préventif du contenu en cache L1 (`ApplyPreventiveShadowbanToPost`).
// @Description       d. Émission d'événements `TrustTriggerJustifiedReport` (+0.03) récompensant les signaleurs citoyens.
// @Description       e. Si le quota de 3 blâmes est atteint par la cible, génération automatique d'un Super-Report d'audit admin (`triggerThreeBlameSystemAudit`).
// @Description       f. Mutation d'état en `ReportStateClosed` (-1) et archivage PostgreSQL L3 (`postgres.FuncArchiveClosedReport`).
// @Description     - **Si `oppose` (Contradiction)** :
// @Description       a. Vérification de l'interrupteur `ModerationAdminDelegationEnabled`.
// @Description       b. Si délégation active : passage en `ReportStatePendingFinalVote` (5) et routage vers la file `ModerationQueueFinalVote` (réservée aux modérateurs avec $T_{\text{mod}} > 0.90$).
// @Description       c. Si délégation inactive : passage en `ReportStateEscalated` (4) et routage vers `ModerationQueueAdmin`.
// @Description  8. Sauvegarde du ticket mis à jour dans Redis (`cache_service.SaveModerationTicket`).
// @Description  9. Renvoi du DTO public `moderation_models.ModerationTicketPublicView`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `moderation_models.ModerationTicketPublicView`.
// @Description  - Persistence guarantees: Archivage direct PostgreSQL en cas de consensus, ou réindexation Redis en file contradictoire en cas d'opposition.
// @Description  - Side effects: Mise à jour du score de réputation de l'auteur et des signaleurs via la file du worker, et déclenchement potentiel d'un audit de quota.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON payload or invalid decision:**
// @Description    - Trigger: Décision absente ou différente de `"validate"` / `"oppose"`.
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
// @Description  - **[FORBIDDEN_ACCESS] Auto-validation prohibited by Four-Eyes principle:**
// @Description    - Trigger: Le modérateur B est identique au modérateur A (`callerID == ticket.ProposerModID`).
// @Description    - Execution stage: Contrôle Four-Eyes dans `ReviewBlameProposalService`.
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
// @Description  - **[CONFLICT] Ticket not awaiting peer review:**
// @Description    - Trigger: Le ticket n'est pas dans l'état `PendingPeerReview` (3).
// @Description    - Execution stage: Vérification de l'état du ticket dans `ReviewBlameProposalService`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeConflict`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Redis or database failure:**
// @Description    - Trigger: Panne de Redis ou de la base PostgreSQL lors de l'archivage de la décision.
// @Description    - Execution stage: Persistance de la revue ou archivage.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                 true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                 true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                 true "Timestamp Unix de la requête"
// @Param        input         body   moderation_models.ReviewBlameInput      true "Décision arbitrale (validate ou oppose) et justification"
// @Success      200  {object}  moderation_models.ModerationTicketPublicView
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Auto-validation impossible)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Ticket Not Found"
// @Failure      409  {object}  numan_error.PublicErrorResponse "Conflict (Wrong state)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/moderation/tickets/blame/review [post]
func ReviewBlameHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}
	grade := c.GetInt("user_grade")

	var input moderation_models.ReviewBlameInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou décision manquante (validate/oppose).", err))
		return
	}

	view, errSvc := moderation_service.ReviewBlameProposalService(c.Request.Context(), callerID, grade, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, view)
}
