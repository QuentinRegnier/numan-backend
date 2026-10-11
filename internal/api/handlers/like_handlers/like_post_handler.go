package like_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/like_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/service/like_service"
	"github.com/gin-gonic/gin"

	"github.com/QuentinRegnier/numan-backend/internal/pkg"
)

// LikePostHandler godoc
// @Summary      Aimer ou désaimer une publication
// @Description  Ajoute ou retire un Like sur une publication. Ce endpoint est asynchrone et ignore volontairement les erreurs métiers ("Fire and Forget") pour maximiser la fluidité côté client.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée (Token JWT requis).
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `post_id` (int64), `action` (chaîne: 'like' ou 'unlike').
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Validation GIN** : Vérifie la structure JSON.
// @Description  2. **Service Fire-and-Forget** : L'appel au service `TogglePostLike` est effectué, et ses potentielles erreurs (Post inexistant) sont volontairement ignorées.
// @Description  3. **Idempotence & Compteur (Interne)** : L'algorithme incrémente ou décrémente `LikeCount` en cache L1, ajoute l'événement dans la file Redis Write-Behind. Le Post est envoyé à l'IA de classement (EvaluatePostAfterLike). Notification poussée à l'auteur (asynchrone).
// @Description  4. **Réponse client** : La confirmation HTTP 200 est envoyée instantanément sans attendre.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON avec `{"message": "Action prise en compte"}`.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request — Payload invalide**
// @Description  - **Trigger:** Le payload JSON est manquant, mal formaté ou l'attribut `action` ne vaut ni `like` ni `unlike`.
// @Description  - **Execution stage:** Phase de binding GIN et validation.
// @Description  - **Response:** Structure JSON `numan_error.PublicErrorResponse`.
// @Description  - **Error code:** `numan_error.CodeInvalidPayload` (renvoyé dans le champ JSON `code`).
// @Description
// @Description  🟠 **401 Unauthorized — Authentification échouée**
// @Description  - **Trigger:** Le token JWT est absent, invalide ou expiré.
// @Description  - **Execution stage:** Middleware d'authentification ou extraction du profil.
// @Description  - **Response:** Structure JSON `numan_error.PublicErrorResponse`.
// @Description  - **Error code:** `numan_error.CodeUnauthorized` (renvoyé dans le champ JSON `code`).
// @Tags         likes
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   like_models.LikePostInput true "Payload pour aimer ou désaimer une publication"
// @Success      200  {object}  map[string]string
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Router       /like/post/set [post]
func LikePostHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	var input like_models.LikePostInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide. 'post_id' et 'action' requis.", err))
		return
	}

	err = like_service.TogglePostLike(c.Request.Context(), callerID, input)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Action prise en compte"})
}
