package map_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/map_service"
	"github.com/gin-gonic/gin"
)

// GetPOIsInBBoxHandler godoc
// @Summary      Récupérer les Points d'Intérêt (POIs) dans une Bounding Box géographique
// @Description  Exécute une recherche spatiale dans le cache Redis GEO L1 (`GEOSEARCH BYBOX`), hydrate les entités depuis l'Object Cache Redis L1 (`MGET`), déclenche une auto-guérison sélective depuis PostgreSQL L3 en cas de cache miss et résout les URLs signées des médias en lot.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide.
// @Description  - Required permissions or roles: Tout utilisateur authentifié.
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `sw_lat` (float64), `sw_lng` (float64), `ne_lat` (float64), `ne_lng` (float64) dans `map_models.GetPoisInBBoxInput`.
// @Description  - Validation rules: `sw_lat <= ne_lat`, `sw_lng <= ne_lng`, latitudes dans `[-90, 90]`, longitudes dans `[-180, 180]`.
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction de l'identifiant de l'appelant (`callerID`) depuis le contexte Gin.
// @Description  2. Parsing et validation du corps JSON (`c.ShouldBindJSON`).
// @Description  3. Délégation à `map_service.GetPOIsInBBoxService`.
// @Description  4. Recherche spatiale L1 dans Redis GEO (`POIsGeo`) et MGET sur `POIs`.
// @Description  5. Auto-guérison L3 (`postgres.FuncGetPOIsByIDs` / `postgres.FuncGetPOIsInBBox`) si nécessaire.
// @Description  6. Résolution groupée des médias (`media_service.FormatMediaViewsCascade`) et construction des vues `POIView`.
// @Description  7. Renvoi du DTO `map_models.GetPoisInBBoxOutput`.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `map_models.GetPoisInBBoxOutput` contenant le tableau `pois` (`[]map_models.POIView`).
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description  - **[INVALID_PAYLOAD] Malformed JSON or invalid bounding box coordinates:**
// @Description    - Trigger: JSON malformé, coordonnées hors bornes WGS84 ou `sw_lat > ne_lat` / `sw_lng > ne_lng`.
// @Description    - Response: `numan_error.PublicErrorResponse` (`numan_error.CodeInvalidPayload`).
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description  - **[UNAUTHORIZED] Missing or invalid JWT:**
// @Description    - Trigger: Token JWT absent ou invalide dans le contexte.
// @Description    - Response: `numan_error.PublicErrorResponse` (`numan_error.CodeUnauthorized`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description  - **[DATABASE_ERROR / CACHE_ERROR] Storage failure:**
// @Description    - Trigger: Indisponibilité simultanée de Redis L1 et PostgreSQL L3.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Tags         map
// @Accept       json
// @Produce      json
// @Param        Authorization header string                        true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                        true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                        true "Timestamp Unix de la requête"
// @Param        input         body   map_models.GetPoisInBBoxInput true "Coordonnées Sud-Ouest et Nord-Est de la Bounding Box"
// @Success      200  {object}  map_models.GetPoisInBBoxOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload or Coordinates"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /map/poi/get [post]
func GetPOIsInBBoxHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	var input map_models.GetPoisInBBoxInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou coordonnées manquantes.", err))
		return
	}

	out, errSvc := map_service.GetPOIsInBBoxService(c.Request.Context(), callerID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
