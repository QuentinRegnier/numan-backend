package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// MongoGetTargetLikes récupère les identifiants utilisateurs ayant liké une cible
// (post = 0, commentaire = 1) depuis le stockage L2 MongoDB (pour réhydratation L1).
func MongoGetTargetLikes(ctx context.Context, targetType int, targetID int64, limit int64) ([]int64, error) {
	if Likes == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Likes' is not initialized"),
		)
	}

	filter := map[string]any{
		"target_type": targetType,
		"target_id":   targetID,
	}
	sort := map[string]any{"created_at": -1}

	docs, err := Likes.GetPaginated(filter, sort, 0, limit)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer les informations des likes.",
			err,
		)
	}

	userIDs := make([]int64, 0, len(docs))
	for _, doc := range docs {
		switch v := doc["user_id"].(type) {
		case float64:
			userIDs = append(userIDs, int64(v))
		case int64:
			userIDs = append(userIDs, v)
		case int32:
			userIDs = append(userIDs, int64(v))
		}
	}

	return userIDs, nil
}

// MongoHasUserLiked vérifie en O(1) dans MongoDB L2 si un utilisateur a déjà liké une cible donnée.
func MongoHasUserLiked(ctx context.Context, targetType int, targetID int64, userID int64) (bool, error) {
	if Likes == nil {
		return false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Likes' is not initialized"),
		)
	}

	filter := map[string]any{
		"target_type": targetType,
		"target_id":   targetID,
		"user_id":     userID,
	}

	docs, err := Likes.GetPaginated(filter, nil, 0, 1)
	if err != nil {
		return false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de vérifier l'existence du like.",
			err,
		)
	}

	return len(docs) > 0, nil
}
