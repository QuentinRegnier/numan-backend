package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"go.mongodb.org/mongo-driver/bson"
)

// MongoGetMessageReactionsPaginated lit la liste des réactions d'un message depuis le L2
func MongoGetMessageReactionsPaginated(ctx context.Context, messageID int64, limit int64, offset int64) ([]message_models.MessageReactionPayload, error) {
	if MessageReactions == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'MessageReactions' is not initialized"),
		)
	}

	filter := bson.M{"message_id": messageID}
	sortMap := map[string]any{"created_at": -1}

	docs, err := MessageReactions.GetPaginated(filter, sortMap, offset, limit)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les réactions du message.",
			err,
		)
	}

	reactions := make([]message_models.MessageReactionPayload, 0, len(docs))
	for _, doc := range docs {
		var r message_models.MessageReactionPayload
		if err := pkg.ToStruct(doc, &r); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de charger les réactions du message.",
				err,
			)
		}
		reactions = append(reactions, r)
	}
	return reactions, nil
}
