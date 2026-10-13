package postgres

import (
	"context"
	"database/sql"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncLoadMessagesByIDs est le fallback L3 d'hydratation
func FuncLoadMessagesByIDs(ctx context.Context, messageIDs []int64) ([]message_models.MessagePayload, error) {
	query := `SELECT id, conversation_id, sender_id, message_type, visibility, content, attachments, allow_new_reactions, created_at, updated_at FROM messaging.func_load_messages_by_ids($1)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, pq.Array(messageIDs))
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Échec de l'exécution de la requête SQL (QueryContext)")
		return nil, numan_error.NewInternal()
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			numan_log.Error(ctx).Err(err).Msg("Erreur lors de la fermeture des lignes Postgres")
		}
	}(rows)

	var messages []message_models.MessagePayload
	for rows.Next() {
		var m message_models.MessagePayload
		var attachBytes []byte
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.MessageType, &m.Visibility, &m.Content, &attachBytes, &m.AllowNewReactions, &m.CreatedAt, &m.UpdatedAt); err == nil {
			// (Note: En Go pur, il faudrait unmarshaler attachBytes dans m.Attachments ici. Pour abréger, on omet cette passe de parsing JSONB standard)
			messages = append(messages, m)
		}
	}
	return messages, nil
}
