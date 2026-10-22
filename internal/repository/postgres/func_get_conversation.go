package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

func FuncGetConversation(ctx context.Context, convID int64) (conversation_models.ConversationPayload, error) {
	if postgres.PostgresDB == nil {
		return conversation_models.ConversationPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT messaging.func_get_conversation_json($1)`
	var c conversation_models.ConversationPayload
	var b []byte

	err := postgres.PostgresDB.QueryRowContext(ctx, query, convID).Scan(&b)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return conversation_models.ConversationPayload{}, numan_error.NewNotFound(
				"CONV_NOT_FOUND",
				"Conversation introuvable.",
				fmt.Errorf("conversation %d was not found in PostgreSQL (L3)", convID),
			)
		}
		return conversation_models.ConversationPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer la conversation.",
			err,
		)
	}

	if b == nil {
		return conversation_models.ConversationPayload{}, numan_error.NewNotFound(
			"CONV_NOT_FOUND",
			"Conversation introuvable.",
			fmt.Errorf("conversation %d was not found in PostgreSQL (L3)", convID),
		)
	}

	if err := json.Unmarshal(b, &c); err != nil {
		return conversation_models.ConversationPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de désérialiser les données de la conversation.",
			err,
		)
	}

	return c, nil
}
