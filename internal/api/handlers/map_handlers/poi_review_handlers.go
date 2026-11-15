package map_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/map_service"
	"github.com/gin-gonic/gin"
)

// SetPOIReviewHandler godoc
// @Summary      Créer ou mettre à jour une note/avis sur un Point d'Intérêt (POI)
// @Description  Enregistre de façon unifiée une note (1 à 5) avec ou sans commentaire textuel pour un POI. Une note sans texte est stockée avec `content = ""`. Chaque utilisateur ne peut déposer qu'un seul avis/note par POI (Upsert). Les compteurs agrégés (`rating`, `comment_count`) sont mis à jour instantanément en RAM L1 et envoyés par lots en arrière-plan vers PostgreSQL L3.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Tout utilisateur authentifié.
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Tags         map
// @Accept       json
// @Produce      json
// @Param        Authorization header string                         true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                         true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                         true "Timestamp Unix de la requête"
// @Param        input         body   map_models.CreatePOIReviewInput true "Identifiant du POI, note (1-5) et commentaire optionnel"
// @Success      200  {object}  map_models.CreatePOIReviewOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      404  {object}  numan_error.PublicErrorResponse "POI Not Found"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /map/poi/review/set [post]
func SetPOIReviewHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input map_models.CreatePOIReviewInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou champs obligatoires manquants (poi_id, rating 1-5).", err))
		return
	}

	out, errSvc := map_service.CreatePOIReviewService(c.Request.Context(), callerID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}

// GetPOIReviewsHandler godoc
// @Summary      Récupérer les avis textuels d'un Point d'Intérêt (POI)
// @Description  Retourne la liste paginée des avis contenant un commentaire textuel (`content != ""`) pour un POI donné. Les notes pures sans texte sont strictement ignorées.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Tout utilisateur authentifié.
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Tags         map
// @Accept       json
// @Produce      json
// @Param        Authorization header string                       true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                       true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                       true "Timestamp Unix de la requête"
// @Param        input         body   map_models.GetPOIReviewsInput true "Identifiant du POI et paramètres de pagination (limit, offset)"
// @Success      200  {object}  map_models.GetPOIReviewsOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      404  {object}  numan_error.PublicErrorResponse "POI Not Found"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /map/poi/review/get [post]
func GetPOIReviewsHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input map_models.GetPOIReviewsInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou poi_id manquant.", err))
		return
	}

	out, errSvc := map_service.GetPOIReviewsService(c.Request.Context(), callerID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}

// DeletePOIReviewHandler godoc
// @Summary      Supprimer l'avis/note de l'utilisateur sur un Point d'Intérêt (POI)
// @Description  Supprime la note ou l'avis de l'utilisateur appelant sur le POI spécifié, purge le cache L1, recalcule la moyenne pondérée et le compteur en RAM, et programme la suppression asynchrone + décrémentation batchée vers PostgreSQL L3.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Tout utilisateur authentifié (propriétaire de l'avis).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Tags         map
// @Accept       json
// @Produce      json
// @Param        Authorization header string                         true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                         true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                         true "Timestamp Unix de la requête"
// @Param        input         body   map_models.DeletePOIReviewInput true "Identifiant du POI"
// @Success      200  {object}  map_models.DeletePOIReviewOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      404  {object}  numan_error.PublicErrorResponse "POI or Review Not Found"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /map/poi/review/delete [delete]
func DeletePOIReviewHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input map_models.DeletePOIReviewInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou poi_id manquant.", err))
		return
	}

	out, errSvc := map_service.DeletePOIReviewService(c.Request.Context(), callerID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
