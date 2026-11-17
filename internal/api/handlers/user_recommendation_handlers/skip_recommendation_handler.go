package user_recommendation_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/user_recommendation_service"
	"github.com/gin-gonic/gin"
)

// SkipUserRecommendationHandler godoc
// @Summary      Ignorer un utilisateur recommandé (Skip)
// @Description  Exclut immédiatement un utilisateur des futures recommandations via le filtre de Cuckoo Redis L1 (`cuckoo:user_reco:skips:{user_id}`) et persiste l'exclusion de manière asynchrone dans MongoDB L2 (`user_reco_skips`).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée (Token JWT + Signature HMAC requis).
// @Description
// @Description  **Request Contract:**
// @Description  - Required JSON field: `skipped_user_id` (`int64 > 0`, différent de l'utilisateur appelant).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Validation du corps JSON et interdiction du self-skip (`callerID == skipped_user_id`).
// @Description  2. Insertion synchrone dans le Cuckoo Filter Redis L1 et retrait immédiat des ZSETs L1.
// @Description  3. Mise en file d'attente asynchrone (`redis.EnqueueDB`) pour persistance dans MongoDB (`user_reco_skips`, TTL 7 jours).
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: `user_recommendation_models.SkipUserRecommendationOutput`.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description  - **[INVALID_PAYLOAD] Payload invalide ou auto-exclusion:**
// @Description    - **Trigger:** Le champ `skipped_user_id` est manquant, `<= 0`, mal formaté, ou égal à l'identifiant de l'utilisateur authentifié.
// @Description    - **Execution stage:** Phase de binding JSON ou validation métier initiale.
// @Description    - **Response:** `numan_error.PublicErrorResponse`.
// @Description    - **Error code:** `numan_error.CodeInvalidPayload`.
// @Description
// @Description  🟠 **401 Unauthorized:**
// @Description  - **[USER_IS_NOT_IDENTIFIED] Utilisateur non identifié:**
// @Description    - **Trigger:** Le contexte ne contient pas d'identifiant utilisateur valide.
// @Description    - **Execution stage:** Vérification d'identité initiale dans le handler.
// @Description    - **Response:** `numan_error.PublicErrorResponse`.
// @Description    - **Error code:** `numan_error.CodeUserIsNotIdentified`.
// @Tags         recommendations
// @Accept       json
// @Produce      json
// @Param        Authorization header string                                                  true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                                                  true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                                                  true "Timestamp Unix de la requête"
// @Param        body          body   user_recommendation_models.SkipUserRecommendationInput  true "Identifiant de l'utilisateur à ignorer"
// @Success      200  {object}  user_recommendation_models.SkipUserRecommendationOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload or Self-Skip"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /users/recommendations/skips [post]
func SkipUserRecommendationHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", err))
		return
	}

	var input user_recommendation_models.SkipUserRecommendationInput
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le JSON de la requête est mal formaté ou incomplet.", errBind))
		return
	}

	output, errService := user_recommendation_service.SkipUserRecommendation(c.Request.Context(), callerID, input)
	if errService != nil {
		numan_error.RespondWithError(c, errService)
		return
	}

	c.JSON(http.StatusOK, output)
}
