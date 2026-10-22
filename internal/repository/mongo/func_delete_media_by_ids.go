package mongo

import (
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// MongoDeleteMediaByIDs effectue un Hard Delete sur le stockage à froid (L2)
func MongoDeleteMediaByIDs(ids []int64) error {
	if Media == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Media' is not initialized"),
		)
	}

	if len(ids) == 0 {
		return nil
	}

	// Utilisation de la méthode Delete abstraite du wrapper (DDD)
	filter := map[string]any{"id": map[string]any{"$in": ids}}
	return Media.Delete(filter)
}
