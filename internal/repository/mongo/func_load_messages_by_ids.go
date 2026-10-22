package mongo

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// MongoLoadMessagesByIDs est le fallback L2 d'hydratation
func MongoLoadMessagesByIDs(ctx context.Context, messageIDs []int64) ([]message_models.MessagePayload, error) {
	if Messages == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Messages' is not initialized"),
		)
	}

	if len(messageIDs) == 0 {
		return []message_models.MessagePayload{}, nil
	}

	filter := map[string]any{"id": map[string]any{"$in": messageIDs}}
	docs, err := Messages.Get(filter, nil)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les messages.",
			err,
		)
	}

	messages := make([]message_models.MessagePayload, 0, len(docs))
	for _, doc := range docs {
		var m message_models.MessagePayload
		if err := pkg.ToStruct(doc, &m); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de traiter les messages.",
				err,
			)
		}
		if _, exists := doc["allow_new_reactions"]; !exists {
			m.AllowNewReactions = variables.ShouldAllowNewReactions(m.MessageType)
		}
		messages = append(messages, m)
	}
	return messages, nil
}
