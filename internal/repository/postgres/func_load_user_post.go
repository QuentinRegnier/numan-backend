package postgres

import (
	"context"
	"database/sql"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncLoadUserPostsKeyset charge les publications d'un utilisateur via content.func_load_user_posts_keyset.
// Le curseur est composé de (beforeCreatedAt, beforeID). Si les deux sont nil, la première page est renvoyée.
func FuncLoadUserPostsKeyset(ctx context.Context, userID int64, beforeCreatedAt *int64, beforeID *int64, limit int64) ([]post_models.PostPayload, error) {
	query := `SELECT * FROM content.func_load_user_posts_keyset($1, $2, $3, $4)`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query, userID, beforeCreatedAt, beforeID, limit)
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

	return scanPosts(ctx, rows)
}
