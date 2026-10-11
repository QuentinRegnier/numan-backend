package saved_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/saved_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/saved_service"
	"github.com/gin-gonic/gin"
)

// GetSavedPostsHandler godoc
// @Summary      Récupérer la liste paginée des favoris
// @Description  Renvoie la liste des publications sauvegardées par l'utilisateur (favoris).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Token requis.
// @Description  - Required permissions or roles: Membre standard.
// @Description  - Relevant middleware: ExtractUserID.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `Limit`, `Offset` passés en Query Params.
// @Description  - Validation rules: Limitation forcée entre 1 et 100 (défaut 50).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. **Extraction** : Récupération sécurisée du `UserID` du caller.
// @Description  2. **Validation GIN** : Binding Query (`ShouldBindQuery`) dans `GetSavedInput`.
// @Description  3. **Bouclier de Pagination** : Forçage strict de la `Limit` à 50 si hors bornes.
// @Description  4. **Cascade de Cache L1 -> L2 -> L3** :
// @Description     - (L1) Tentative de lecture en RAM via Redis ZSET (O(log N)).
// @Description     - (L2) En cas d'échec ou d'absence, lecture sur MongoDB et auto-guérison du cache L1 (ajout au ZSET).
// @Description     - (L3) En cas d'échec L2, lecture sur PostgreSQL, auto-guérison L1 synchrone et L2 asynchrone via Workers.
// @Description  5. **Coupe-circuit** : Si aucun favori n'est trouvé, retour immédiat (tableau vide).
// @Description  6. **Hydratation massive** : Délégation au `post_service.GetPosts` avec les IDs récupérés pour résoudre les vues des posts (vérification des droits, génération des URLs S3 signées, hydratation des commentaires).
// @Description  7. **Réponse HTTP** : Renvoi du tableau hydraté au format JSON.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: Liste de `post_models.GetPostOutput`.
// @Description  - Persistence guarantees: Lecture uniquement avec auto-réparation de cache (Cache-Aside & Self-Healing).
// @Description  - Side effects: Mise à jour potentielle des caches L1/L2 si donnés absentes (Auto-guérison).
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_QUERY] Paramètres invalides:**
// @Description    - Trigger: Les paramètres Query `limit` ou `offset` sont impossibles à parser.
// @Description    - Execution stage: Validation GIN (`ShouldBindQuery`).
// @Description    - Response: `numan_error.PublicErrorResponse` ("Paramètres de requête invalides.").
// @Description    - Error code: `INVALID_QUERY` (ou `numan_error.CodeInvalidPayload`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_ERROR] Échec L3:**
// @Description    - Trigger: PostgreSQL (L3) est inaccessible après échec L1/L2.
// @Description    - Execution stage: Récupération des IDs sauvegardés.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternal`.
// @Tags         saved
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   saved_models.GetSavedInput true "Paramètres de pagination pour récupérer les favoris de l'utilisateur"
// @Success      200  {array}  post_models.GetPostOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Query Parameters"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /saved/get [get]
func GetSavedPostsHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	var input saved_models.GetSavedInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou paramètres manquants.", err))
		return
	}

	results, err := saved_service.GetSavedPosts(c.Request.Context(), callerID, input)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	c.JSON(http.StatusOK, results)
}
