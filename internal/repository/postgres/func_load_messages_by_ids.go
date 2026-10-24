package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncLoadMessagesByIDs est le fallback L3 d'hydratation
func FuncLoadMessagesByIDs(ctx context.Context, messageIDs []int64) ([]message_models.MessagePayload, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT id, conversation_id, sender_id, message_type, visibility, content, attachments, allow_new_reactions, created_at, updated_at FROM messaging.func_load_messages_by_ids($1)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, pq.Array(messageIDs))
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les messages par identifiants.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	messages := make([]message_models.MessagePayload, 0)
	for rows.Next() {
		var m message_models.MessagePayload
		var attachBytes []byte
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.MessageType, &m.Visibility, &m.Content, &attachBytes, &m.AllowNewReactions, &m.CreatedAt, &m.UpdatedAt); err == nil {
			messages = append(messages, m)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les messages.",
			err,
		)
	}

	return messages, nil
}
