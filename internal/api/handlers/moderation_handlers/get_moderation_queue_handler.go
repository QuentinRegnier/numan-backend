package moderation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/gin-gonic/gin"
)

// GetModerationQueueHandler godoc
// @Summary      Consulter la file de modération (Médias en quarantaine et signalements)
// @Description  Permet aux modérateurs et administrateurs d'extraire la file de modération temps réel du backend Numan. Cette route permet d'interroger soit la file active générale ordonnée par le score maître $P_{\text{file}}$ en $O(1)$ RAM (Redis ZSET), soit les sous-files spécialisées de gouvernance (`peer_review`, `final_vote`, `admin`, `waiting_info`), tout en agrégeant les médias bloqués en quarantaine (`visibility = 2`) en attente de revue.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Le compte appelant doit posséder au minimum le grade Modérateur (`UserGradeModerator = 3`) ou Administrateur (`UserGradeAdmin = 4`). Pour la sous-file `final_vote`, une fiabilité $T_{\text{mod}} \ge 0.90$ est requise pour les modérateurs.
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Optional fields: `queue_type` (string : `"active"`, `"peer_review"`, `"final_vote"`, `"admin"`, `"waiting_info"`), `limit` (int, pagination entre 1 et 100, défaut 50), `offset` (int, défaut 0).
// @Description  - Validation rules: Validation structurelle via `c.ShouldBindJSON` si un corps JSON est fourni.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction et validation de l'identifiant du modérateur (`callerID`) depuis le contexte Gin sécurisé (`pkg.GetUserIDFromContext`).
// @Description  2. Contrôle d'accès Zero-Trust en RAM L1 (`cache_service.GetUserLite`) vérifiant le grade Modérateur ou Administrateur.
// @Description  3. Parsing et normalisation défensive des curseurs de pagination (`limit` borné entre 1 et 100, `offset` positif).
// @Description  4. Si `queue_type` est spécifié : extraction ordonnée depuis le Redis ZSET ciblé (`ZREVRANGEBYSCORE` paginé) et hydratation des tickets en mémoire L1 (`cache_service.GetModerationTicket`).
// @Description  5. Si consultation générale (file active) : extraction conjointe des médias en quarantaine (`postgres.FuncLoadQuarantinedMediaPaginated`) et des signalements en attente (`postgres.FuncLoadPendingReportsPaginated` ou Redis ZSET).
// @Description  6. Hydratation en mémoire O(1) RAM des profils auteurs des médias en quarantaine via le Speed Cache L1 (`cache_service.GetUserLite`) pour joindre en direct le `TrustScore` de chaque auteur au DTO.
// @Description  7. Assemblage et renvoi de la charge utile consolidée `moderation_models.GetModerationQueueOutput`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `moderation_models.GetModerationQueueOutput` contenant `quarantined_media` et/ou `queue_tickets`.
// @Description  - Persistence guarantees: Opération de lecture pure en RAM Redis et Cold Storage PostgreSQL L3 sans mutation d'état.
// @Description  - Side effects: Aucun effet de bord ; consultation passive du stock de modération.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON pagination payload:**
// @Description    - Trigger: Le corps de la requête est non vide mais ne respecte pas le schéma JSON attendu.
// @Description    - Execution stage: Phase de binding GIN (`c.ShouldBindJSON`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload`.
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED] Missing or invalid user identity:**
// @Description    - Trigger: En-tête Authorization manquant, jeton JWT expiré ou identifiant utilisateur impossible à extraire.
// @Description    - Execution stage: Extraction du contexte (`pkg.GetUserIDFromContext`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized`.
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Insufficient privileges:**
// @Description    - Trigger: L'utilisateur authentifié possède un grade inférieur à Modérateur (`UserGradeModerator = 3`), ou tente d'accéder à `final_vote` avec $T_{\text{mod}} < 0.90$.
// @Description    - Execution stage: Vérification des droits au sein de `moderation_service.GetModerationQueue`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Database or cache error:**
// @Description    - Trigger: Défaillance de connectivité réseau vers Redis ou PostgreSQL.
// @Description    - Execution stage: Lecture des files ou des tables PostgreSQL.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                      true  "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                      true  "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                      true  "Timestamp Unix de la requête"
// @Param        input         body   moderation_models.GetModerationQueueInput  false "Paramètres de pagination (limit, offset)"
// @Success      200  {object}  moderation_models.GetModerationQueueOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Moderator or Admin required)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/moderation/queue [post]
func GetModerationQueueHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input moderation_models.GetModerationQueueInput
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&input); err != nil {
			numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide.", err))
			return
		}
	}
	if input.Limit == 0 {
		input.Limit = 50
	}

	output, errSvc := moderation_service.GetModerationQueue(c.Request.Context(), callerID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, output)
}
