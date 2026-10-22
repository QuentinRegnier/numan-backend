package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncGetMessageReactionsPaginated lit la liste des réactions d'un message depuis le L3 via messaging.func_get_message_reactions_keyset.
func FuncGetMessageReactionsPaginated(ctx context.Context, messageID int64, beforeCreatedAt *int64, beforeID *int64, limit int) ([]message_models.MessageReactionPayload, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT id, message_id, user_id, reaction, created_at FROM messaging.func_get_message_reactions_keyset($1, $2, $3, $4)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, messageID, beforeCreatedAt, beforeID, limit)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les réactions du message.",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	reactions := make([]message_models.MessageReactionPayload, 0)
	for rows.Next() {
		var r message_models.MessageReactionPayload
		if err := rows.Scan(&r.ID, &r.MessageID, &r.UserID, &r.Reaction, &r.CreatedAt); err == nil {
			reactions = append(reactions, r)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les réactions du message.",
			err,
		)
	}

	return reactions, nil
}
