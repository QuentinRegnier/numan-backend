package mongo

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
)

// MongoGetConversation récupère une conversation depuis le Warm Storage (L2).
func MongoGetConversation(ctx context.Context, convID int64) (conversation_models.ConversationPayload, error) {
	if Conversations == nil {
		return conversation_models.ConversationPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("MongoDB collection 'Conversations' is not initialized"),
		)
	}

	filter := map[string]any{"id": convID}
	docs, err := Conversations.Get(filter, nil)
	if err != nil {
		return conversation_models.ConversationPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les données de la conversation.",
			err,
		)
	}

	if len(docs) == 0 {
		return conversation_models.ConversationPayload{}, numan_error.NewNotFound(
			"CONV_NOT_FOUND",
			"Conversation introuvable (Storage L2).",
			fmt.Errorf("conversation %d was not found in Warm Storage (L2)", convID),
		)
	}

	var conv conversation_models.ConversationPayload
	if err := pkg.ToStruct(docs[0], &conv); err != nil {
		return conversation_models.ConversationPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les données de la conversation.",
			err,
		)
	}

	return conv, nil
}
