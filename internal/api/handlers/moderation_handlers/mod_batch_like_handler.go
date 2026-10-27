package moderation_handlers

import (
	"encoding/json"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
)

// ModBatchLikeHandler godoc
// @Summary      Validation groupée de publications / Mod Batch Like (+0.02 Trust Score)
// @Description  Permet aux modérateurs et administrateurs d'attribuer collectivement un bonus de réputation (+0.02) à un lot d'auteurs dont les contenus qualitatifs ont été validés. Dépose un événement de masse unique dans la file Redis du Worker (`variables.WorkerQueueTrustScore`) pour traitement asynchrone par lots haute performance (`trust_score_worker.go`) et application en base de données L2/L3.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Modérateur (`UserGradeModerator = 3`) ou Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `author_user_ids` ([]int64, liste non vide d'identifiants utilisateurs) dans le corps JSON (`moderation_models.ModBatchLikeInput`).
// @Description  - Optional fields: `reason` (string, motif optionnel de la validation groupée).
// @Description  - Validation rules: `author_user_ids` doit comporter au moins 1 identifiant (`binding:"required,min=1"`).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction et validation de l'identifiant de l'appelant depuis le contexte Gin (`pkg.GetUserIDFromContext`).
// @Description  2. Désérialisation et validation du corps de requête JSON (`c.ShouldBindJSON`).
// @Description  3. Génération d'un identifiant d'événement unique Snowflake et capture de l'horodatage courant en millisecondes.
// @Description  4. Construction du payload de réputation de masse `moderation_models.TrustScoreEventPayload` avec le déclencheur canonique `variables.TrustTriggerModBatchLike` (+0.02).
// @Description  5. Sérialisation JSON de l'événement et publication non-bloquante dans la file d'attente Redis `variables.WorkerQueueTrustScore` (`redis.WorkerQueue.LPush`).
// @Description  6. Renvoi du DTO de confirmation `moderation_models.ModBatchLikeOutput` contenant l'ID d'événement, le nombre d'auteurs gratifiés et l'horodatage d'enfilement.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `moderation_models.ModBatchLikeOutput`.
// @Description  - Persistence guarantees: Enfilement atomique dans la file Redis du Worker (FIFO).
// @Description  - Side effects: Dépilage et application asynchrone par le Worker de réputation avec recalcul ELO amorti pour chaque utilisateur et mise à jour L1/L2/L3.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON payload or empty author list:**
// @Description    - Trigger: Le corps JSON est malformé ou la liste `author_user_ids` est vide.
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
// @Description    - Trigger: L'appelant ne possède pas le grade Modérateur ou Administrateur.
// @Description    - Execution stage: Contrôle des privilèges d'accès.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden`.
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Redis queue push failure:**
// @Description    - Trigger: Rupture de connectivité réseau avec le serveur Redis lors du push dans la file de réputation.
// @Description    - Execution stage: Enfilement dans `redis.WorkerQueue`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeCacheError` ou `numan_error.CodeInternalError`.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                  true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                  true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                  true "Timestamp Unix de la requête"
// @Param        input         body   moderation_models.ModBatchLikeInput      true "Liste des identifiants utilisateurs auteurs et motif optionnel"
// @Success      200  {object}  moderation_models.ModBatchLikeOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/moderation/batch-like [post]
func ModBatchLikeHandler(c *gin.Context) {
	_, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input moderation_models.ModBatchLikeInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou liste d'auteurs vide.", err))
		return
	}

	nowMs := domain.NowMillis()
	eventID := pkg.GenerateID()

	evt := moderation_models.TrustScoreEventPayload{
		EventID:      eventID,
		UserIDs:      input.AuthorUserIDs,
		EventType:    variables.TrustTriggerModBatchLike,
		Reason:       input.Reason,
		OccurredAtMs: nowMs,
	}

	payloadBytes, errMarshal := json.Marshal(evt)
	if errMarshal != nil {
		numan_error.RespondWithError(c, numan_error.NewInternal(errMarshal))
		return
	}

	if errQueue := redis.WorkerQueue.LPush(c.Request.Context(), variables.WorkerQueueTrustScore, payloadBytes); errQueue != nil {
		numan_error.RespondWithError(c, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Impossible d'enregistrer l'action groupée dans la file de réputation.", errQueue))
		return
	}

	c.JSON(http.StatusOK, moderation_models.ModBatchLikeOutput{
		EventID:    eventID,
		UsersCount: len(input.AuthorUserIDs),
		QueuedAt:   nowMs,
	})
}
