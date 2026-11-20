package article_handlers

import (
	"errors"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/gin-gonic/gin"
)

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
