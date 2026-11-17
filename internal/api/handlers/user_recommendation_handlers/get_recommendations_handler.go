package user_recommendation_handlers

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/user_recommendation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
)

// GetUserRecommendationsHandler godoc
// @Summary      Obtenir les recommandations d'utilisateurs
// @Description  Récupère de manière ultra-rapide (L1 Redis ZSET, < 5ms) la liste paginée de profils d'utilisateurs recommandés et hydratés (`UserLiteView`).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée (Token JWT + Signature HMAC requis). L'identifiant de l'utilisateur appelant est extrait exclusivement du contexte JWT.
// @Description
// @Description  **Request Contract:**
// @Description  - HTTP Method: `POST` (aucun paramètre dans l'URL, tous les paramètres métier sont passés dans le corps JSON).
// @Description  - JSON Body (`user_recommendation_models.GetUserRecommendationsInput`) :
// @Description    - `sort` (optionnel, défaut `global`, valeurs autorisées : `global`, `geo`, `aff`, `taste`)
// @Description    - `limit` (optionnel, défaut `20`, min `1`, max `50`, type `int64`)
// @Description  - Variante `/users/recommendations/force` : déclenche explicitement le mode `force=true` sans paramètre d'URL.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identifiant appelant depuis le contexte JWT (`pkg.GetUserIDFromContext`).
// @Description  2. Parsing et validation du corps JSON (`sort`, `limit`).
// @Description  3. Lecture de la liste triée Redis (`user:reco:list:{sort}:{user_id}`). En cas de Cache Miss initial, déclenchement du calcul asynchrone via `redis.EnqueueDB` (`ActionGenerateUserRecommendations`).
// @Description  4. Hydratation des profils via `UserLiteView` en préservant strictement l'ordre de classement Redis.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: `user_recommendation_models.GetUserRecommendationsOutput` contenant la tranche `users` (`[]auth_models.UserLiteView`).
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description  - **[INVALID_PAYLOAD] Corps JSON ou paramètres métier invalides:**
// @Description    - **Trigger:** Le corps JSON est mal formaté, `sort` n'appartient pas à `{global, geo, aff, taste}`, ou `limit` est hors de `[1, 50]`.
// @Description    - **Execution stage:** Phase de binding JSON et validation initiale dans le handler.
// @Description    - **Response:** `numan_error.PublicErrorResponse`.
// @Description    - **Error code:** `numan_error.CodeInvalidPayload`.
// @Description
// @Description  🟠 **401 Unauthorized:**
// @Description  - **[USER_IS_NOT_IDENTIFIED] Utilisateur non identifié:**
// @Description    - **Trigger:** Le contexte ne contient pas d'identifiant utilisateur valide issu du middleware JWT.
// @Description    - **Execution stage:** Vérification d'identité initiale dans le handler.
// @Description    - **Response:** `numan_error.PublicErrorResponse`.
// @Description    - **Error code:** `numan_error.CodeUserIsNotIdentified`.
// @Tags         recommendations
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                                 true  "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                                 true  "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                                 true  "Timestamp Unix de la requête"
// @Param        body          body   user_recommendation_models.GetUserRecommendationsInput false "Paramètres de tri et de limite (sort, limit)"
// @Success      200  {object}  user_recommendation_models.GetUserRecommendationsOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid JSON Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /users/recommendations [post]
func GetUserRecommendationsHandler(c *gin.Context) {
	handleGetUserRecommendations(c, false)
}

// ForceUserRecommendationsHandler godoc
// @Summary      Forcer le recalcul et obtenir les recommandations d'utilisateurs
// @Description  Variante explicite `/users/recommendations/force` qui force la régénération asynchrone des recommandations (protégée par le verrou atomique `lock:user_reco:compute:{user_id}` de 12h) et retourne la liste paginée de profils recommandés (`UserLiteView`).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée (Token JWT + Signature HMAC requis). L'identifiant de l'utilisateur appelant est extrait exclusivement du contexte JWT.
// @Description
// @Description  **Request Contract:**
// @Description  - HTTP Method: `POST` (aucun paramètre dans l'URL, tous les paramètres métier sont passés dans le corps JSON).
// @Description  - JSON Body (`user_recommendation_models.GetUserRecommendationsInput`) :
// @Description    - `sort` (optionnel, défaut `global`, valeurs autorisées : `global`, `geo`, `aff`, `taste`)
// @Description    - `limit` (optionnel, défaut `20`, min `1`, max `50`, type `int64`)
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identifiant appelant depuis le contexte JWT (`pkg.GetUserIDFromContext`).
// @Description  2. Parsing et validation du corps JSON (`sort`, `limit`) et activation explicite de `Force = true`.
// @Description  3. Vérification atomique du verrou `lock:user_reco:compute:{user_id}` (TTL 12h) et déclenchement asynchrone via `redis.EnqueueDB` (`ActionGenerateUserRecommendations`).
// @Description  4. Lecture de la liste triée Redis (`user:reco:list:{sort}:{user_id}`) et hydratation des profils via `UserLiteView` en préservant strictement l'ordre de classement Redis.
// @Tags         recommendations
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                                 true  "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                                 true  "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                                 true  "Timestamp Unix de la requête"
// @Param        body          body   user_recommendation_models.GetUserRecommendationsInput false "Paramètres de tri et de limite (sort, limit)"
// @Success      200  {object}  user_recommendation_models.GetUserRecommendationsOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid JSON Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /users/recommendations/force [post]
func ForceUserRecommendationsHandler(c *gin.Context) {
	handleGetUserRecommendations(c, true)
}

func handleGetUserRecommendations(c *gin.Context, force bool) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	input := user_recommendation_models.GetUserRecommendationsInput{
		Sort:  variables.UserRecoSortGlobal,
		Limit: variables.UserRecoDefaultLimit,
	}

	if c.Request != nil && c.Request.Body != nil && c.Request.ContentLength != 0 {
		if errBind := c.ShouldBindJSON(&input); errBind != nil && !errors.Is(errBind, io.EOF) {
			numan_error.RespondWithError(c, numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				"Le corps JSON de la requête est mal formaté ou contient des paramètres invalides.",
				errBind,
			))
			return
		}
	}

	input.Sort = strings.TrimSpace(input.Sort)
	input.Force = force

	if errValidate := input.Validate(); errValidate != nil {
		numan_error.RespondWithError(c, errValidate)
		return
	}

	output, errService := user_recommendation_service.GetUserRecommendations(c.Request.Context(), callerID, input)
	if errService != nil {
		numan_error.RespondWithError(c, errService)
		return
	}

	c.JSON(http.StatusOK, output)
}
