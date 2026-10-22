package mongo

import (
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"go.mongodb.org/mongo-driver/bson"
)

func MongoLoadMembersByRolePaginated(convID int64, role int, limit int64, offset int64) ([]member_models.MemberPayload, error) {
	if Members == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Members' is not initialized"),
		)
	}

	filter := bson.M{"conversation_id": convID, "role": role}
	sort := bson.M{"joined_at": -1} // Du plus récent au plus ancien

	docs, err := Members.GetPaginated(filter, sort, offset, limit)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les membres.",
			err,
		)
	}

	members := make([]member_models.MemberPayload, 0, len(docs))
	for _, doc := range docs {
		var m member_models.MemberPayload
		if err := pkg.ToStruct(doc, &m); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de traiter les membres.",
				err,
			)
		}
		members = append(members, m)
	}
	return members, nil
}
