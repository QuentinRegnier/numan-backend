package mongo

import (
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"go.mongodb.org/mongo-driver/bson"
)

// MongoGetPinnedIndices récupère tous les indices d'épingles utilisés par l'utilisateur.
func MongoGetPinnedIndices(userID int64) ([]int, error) {
	if Members == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Members' is not initialized"),
		)
	}

	filter := bson.M{
		"user_id":         userID,
		"settings.pinned": bson.M{"$gte": 0},
	}
	docs, err := Members.GetPaginated(filter, nil, 0, 3)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer les indices d'épingles.",
			err,
		)
	}

	indices := []int{}
	for _, doc := range docs {
		var mem member_models.MemberPayload
		if err := pkg.ToStruct(doc, &mem); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de traiter les indices d'épingles.",
				err,
			)
		}
		if mem.Settings.Pinned >= 0 {
			indices = append(indices, mem.Settings.Pinned)
		}
	}
	return indices, nil
}
