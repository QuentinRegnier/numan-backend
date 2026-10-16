package postgres

import (
	"context"
	"encoding/json"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

func FuncGetConversation(ctx context.Context, convID int64) (conversation_models.ConversationPayload, error) {
	query := `SELECT messaging.func_get_conversation_json($1)`
	var c conversation_models.ConversationPayload
	var b []byte

	err := postgres.PostgresDB.QueryRowContext(ctx, query, convID).Scan(&b)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Erreur SQL lors de la récupération de la conversation")
		return c, numan_error.NewInternal(err)
	}

	if b == nil {
		return c, numan_error.NewNotFound("CONV_NOT_FOUND", "Conversation introuvable.", nil)
	}

	err = json.Unmarshal(b, &c)
	if err != nil {
		numan_log.Warn(ctx).Err(err).Msg("Erreur lors de la désérialisation de la conversation")
		return c, numan_error.NewInternal(err)
	}

	return c, nil
}
