package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// MongoGetPostLikes interroge le stockage L2 pour la liste des likes.
func MongoGetPostLikes(ctx context.Context, postID int64, limit int64, offset int64) ([]int64, error) {
	if Likes == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Likes' is not initialized"),
		)
	}

	// Cible explicitement les Posts (0) et l'ID
	filter := map[string]any{
		"target_type": 0,
		"target_id":   postID,
	}

	// On trie par date de création décroissante (les plus récents en premier)
	sort := map[string]any{"created_at": -1}

	docs, err := Likes.GetPaginated(filter, sort, offset, limit)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer les informations des likes.",
			err,
		)
	}

	userIDs := []int64{}
	for _, doc := range docs {
		if uidFloat, ok := doc["user_id"].(float64); ok {
			userIDs = append(userIDs, int64(uidFloat))
		} else if uidInt64, ok := doc["user_id"].(int64); ok {
			userIDs = append(userIDs, uidInt64)
		} else if uidInt32, ok := doc["user_id"].(int32); ok {
			userIDs = append(userIDs, int64(uidInt32))
		}
	}

	return userIDs, nil
}
