package map_handlers

import (
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/map_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
)

// ensureAdminPrivilege vérifie que le requérant possède le grade Administrateur lorsque user_grade est présent dans le contexte Gin.
func ensureAdminPrivilege(c *gin.Context) error {
	if rawGrade, exists := c.Get("user_grade"); exists {
		if grade, ok := rawGrade.(int); ok {
			if grade != variables.UserGradeAdmin && grade != variables.MemberRoleAdmin {
				return numan_error.NewForbidden(numan_error.CodeForbidden, "Privilèges d'administrateur requis.", nil)
			}
		}
	}
	return nil
}

// ImportPOIsHandler godoc
// @Summary      Importer un lot de Points d'Intérêt (Admin)
// @Description  Permet exclusivement à un Administrateur d'importer en masse des Points d'Intérêt (POIs). Valide l'intégralité du lot, génère les identifiants Snowflake manquants, met à jour de manière synchrone l'Object Cache Redis L1 (`POIs`, TTL 0) ainsi que l'index spatial Redis GEO (`POIsGeo`, TTL 0), puis ordonnance la persistance asynchrone Write-Behind vers PostgreSQL L3 (`TargetPostgres`).
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée Admin. Requiert un JWT valide, une signature HMAC valide et l'appartenance au sous-réseau WireGuard.
// @Description  - Required permissions or roles: Grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: JWTMiddleware, HMACMiddleware, AdminGuard.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: `pois` (`[]map_models.CreatePOIItemInput`, min 1, max 1000).
// @Description  - Validation rules: Chaque POI doit avoir un `title` non vide, une `description` <= 500 caractères et des coordonnées WGS84 valides.
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `map_models.ImportPOIsOutput`.
// @Tags         admin,map
// @Accept       json
// @Produce      json
// @Param        Authorization header string                      true "Bearer <votre_jwt>"
// @Param        X-Signature   header string                      true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string                      true "Timestamp Unix de la requête"
// @Param        input         body   map_models.ImportPOIsInput  true "Lot de POIs à importer"
// @Success      200  {object}  map_models.ImportPOIsOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin only)"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /admin/map/poi/import [post]
func ImportPOIsHandler(c *gin.Context) {
	adminID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}
	if errAdmin := ensureAdminPrivilege(c); errAdmin != nil {
		numan_error.RespondWithError(c, errAdmin)
		return
	}

	var input map_models.ImportPOIsInput
	if err := c.ShouldBindJSON(&input); err != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide ou liste de POIs manquante.", err))
		return
	}

	out, errSvc := map_service.ImportPOIsService(c.Request.Context(), adminID, input)
	if errSvc != nil {
		numan_error.RespondWithError(c, errSvc)
		return
	}

	c.JSON(http.StatusOK, out)
}
