package map_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/map_service"
	"github.com/gin-gonic/gin"
)

// DeletePOIHandler godoc
// @Summary      Supprimer un ou plusieurs Points d'Intérêt (Admin)
// @Description  Permet exclusivement à un Administrateur de supprimer un ou plusieurs Points d'Intérêt (`[]POI_IDs`). Purge immédiatement les objets du cache Redis L1 (`POIs`) ainsi que leurs entrées dans l'index spatial Redis GEO (`POIsGeo` via `ZREM`), puis ordonnance la suppression asynchrone en PostgreSQL L3 (`TargetPostgres`).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée Admin (JWT + HMAC + WireGuard).
// @Description  - Required permissions or roles: Grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: JWTMiddleware, HMACMiddleware, AdminGuard.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `poi_ids` (`[]int64`, min 1) dans `map_models.DeletePOIInput`.
// @Description  - Validation rules: Tous les identifiants doivent être strictement positifs.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `map_models.DeletePOIOutput`.
// @Tags         admin,map
// @Accept       json
// @Produce      json
// @Param        Authorization header string                     true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                     true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                     true "Timestamp Unix de la requête"
// @Param        input         body   map_models.DeletePOIInput  true "Liste des identifiants de POIs à supprimer"
// @Success      200  {object}  map_models.DeletePOIOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin only)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/map/poi/delete [delete]
func DeletePOIHandler(c *gin.Context) {
	adminID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}
	if errAdmin := ensureAdminPrivilege(c); errAdmin != nil {
		numan_error.RespondWithError(c, errAdmin)
		return
	}

	var input map_models.DeletePOIInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou liste d'identifiants manquante.", err))
		return
	}

	out, errSvc := map_service.DeletePOIsService(c.Request.Context(), adminID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
