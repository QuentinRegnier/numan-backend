package map_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/map_service"
	"github.com/gin-gonic/gin"
)

// UpdatePOIHandler godoc
// @Summary      Mettre à jour un ou plusieurs Points d'Intérêt (Admin)
// @Description  Permet exclusivement à un Administrateur de modifier un ou plusieurs Points d'Intérêt existants. Met à jour de manière synchrone l'Object Cache Redis L1 (`POIs`) ainsi que la position dans l'index spatial Redis GEO (`POIsGeo`), puis ordonnance la mise à jour asynchrone vers PostgreSQL L3 (`TargetPostgres`).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée Admin (JWT + HMAC + WireGuard).
// @Description  - Required permissions or roles: Grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: JWTMiddleware, HMACMiddleware, AdminGuard.
// @Description
// @Description  **Request Contract:**
// @Description  - Accepts a single POI update at root (`id` > 0) or a batch under `pois` (`map_models.UpdatePOIInput`).
// @Description  - Validation rules: `id` strictement positif, `description` <= 500 caractères, coordonnées WGS84 valides.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `map_models.UpdatePOIOutput`.
// @Tags         admin,map
// @Accept       json
// @Produce      json
// @Param        Authorization header string                     true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                     true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                     true "Timestamp Unix de la requête"
// @Param        input         body   map_models.UpdatePOIInput  true "Modifications à appliquer sur le(s) POI(s)"
// @Success      200  {object}  map_models.UpdatePOIOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin only)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "POI Not Found"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/map/poi/update [put]
func UpdatePOIHandler(c *gin.Context) {
	adminID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}
	if errAdmin := ensureAdminPrivilege(c); errAdmin != nil {
		numan_error.RespondWithError(c, errAdmin)
		return
	}

	var input map_models.UpdatePOIInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou paramètres manquants.", err))
		return
	}

	out, errSvc := map_service.UpdatePOIsService(c.Request.Context(), adminID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
