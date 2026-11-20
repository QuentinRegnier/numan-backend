package article_handlers

import (
	"errors"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
)

// isCallerAdmin détermine si l'utilisateur authentifié possède le rôle Administrateur (pour la visibilité des brouillons et articles expirés).
func isCallerAdmin(c *gin.Context, callerID int64) bool {
	if rawGrade, exists := c.Get("user_grade"); exists {
		if grade, ok := rawGrade.(int); ok {
			return grade == variables.UserGradeAdmin || grade == variables.MemberRoleAdmin
		}
	}
	if callerID > 0 {
		if userLite, err := cache_service.GetUserLite(c.Request.Context(), callerID); err == nil && userLite.ID > 0 {
			return userLite.Grade == variables.UserGradeAdmin || userLite.Grade == variables.MemberRoleAdmin
		}
	}
	return false
}

// rejectQueryParameters applique la règle stricte make-perfect-route : aucun paramètre ne doit transiter dans la query string de l'URL.
func rejectQueryParameters(c *gin.Context) error {
	if c.Request != nil && c.Request.URL != nil && c.Request.URL.RawQuery != "" {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Les paramètres d'URL (query parameters) sont interdits. Utilisez le corps de la requête.",
			errors.New("unexpected query parameters in request URL"),
		)
	}
	return nil
}
