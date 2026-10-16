package postgres

import (
	"context"
	"database/sql"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncGetMessageReactionsPaginated lit la liste des réactions d'un message depuis le L3 via messaging.func_get_message_reactions_keyset.
func FuncGetMessageReactionsPaginated(ctx context.Context, messageID int64, beforeCreatedAt *int64, beforeID *int64, limit int) ([]message_models.MessageReactionPayload, error) {
	query := `SELECT id, message_id, user_id, reaction, created_at FROM messaging.func_get_message_reactions_keyset($1, $2, $3, $4)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, messageID, beforeCreatedAt, beforeID, limit)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Échec de l'exécution de la requête SQL (QueryContext)")
		return nil, numan_error.NewInternal()
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			numan_log.Error(ctx).Err(err).Msg("Erreur lors de la fermeture des lignes Postgres (Reactions)")
		}
	}(rows)

	var reactions []message_models.MessageReactionPayload
	for rows.Next() {
		var r message_models.MessageReactionPayload
		if err := rows.Scan(&r.ID, &r.MessageID, &r.UserID, &r.Reaction, &r.CreatedAt); err == nil {
			reactions = append(reactions, r)
		}
	}
	if err := rows.Err(); err != nil {
		numan_log.Error(ctx).Err(err).Msg("Erreur lors de l'itération sur les résultats SQL (rows.Err)")
		return nil, numan_error.NewInternal()
	}
	return reactions, nil
}
