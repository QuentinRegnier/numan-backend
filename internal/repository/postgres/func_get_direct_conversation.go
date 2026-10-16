package postgres

import (
	"context"
	"encoding/json"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

func FuncGetDirectConversation(ctx context.Context, user1, user2 int64) (conversation_models.ConversationPayload, error) {
	query := `SELECT messaging.func_get_direct_conversation_json($1, $2)`
	var c conversation_models.ConversationPayload
	var b []byte

	err := postgres.PostgresDB.QueryRowContext(ctx, query, user1, user2).Scan(&b)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Erreur SQL lors de la récupération de la conversation privée")
		return c, numan_error.NewInternal(err)
	}

	if b == nil {
		return c, numan_error.NewNotFound("CONV_NOT_FOUND", "Conversation privée introuvable.", nil)
	}

	err = json.Unmarshal(b, &c)
	if err != nil {
		numan_log.Warn(ctx).Err(err).Msg("Erreur lors de la désérialisation de la conversation privée")
		return c, numan_error.NewInternal(err)
	}

	return c, nil
}
