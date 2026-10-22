package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"go.mongodb.org/mongo-driver/bson"
)

// MongoGetFirebaseInstallationIDs récupère tous les tokens de notification d'un utilisateur dans le stockage à froid
func MongoGetFirebaseInstallationIDs(ctx context.Context, userID int64) ([]string, error) {
	if Sessions == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Sessions' is not initialized"),
		)
	}

	filter := bson.M{"user_id": userID}
	docs, err := Sessions.Get(filter, nil)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer les informations de session.",
			err,
		)
	}

	tokens := []string{}
	for _, doc := range docs {
		if token, ok := doc["firebase_installation_id"].(string); ok && token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens, nil
}
