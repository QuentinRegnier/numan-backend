package postgres

import (
	"context"
	"database/sql"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/saved_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncLoadSavedPostsKeyset charge les publications sauvegardées via content.func_load_saved_posts_keyset.
func FuncLoadSavedPostsKeyset(ctx context.Context, userID int64, beforeCreatedAt *int64, beforeID *int64, limit int64) ([]saved_models.SavedPayload, error) {
	query := `SELECT id, user_id, post_id, created_at FROM content.func_load_saved_posts_keyset($1, $2, $3, $4)`
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

	var saveds []saved_models.SavedPayload
	for rows.Next() {
		var s saved_models.SavedPayload
		if err := rows.Scan(&s.ID, &s.UserID, &s.PostID, &s.CreatedAt); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Erreur lors du scan d'un post sauvegardé (Postgres)")
			return nil, numan_error.NewInternal()
		}
		saveds = append(saveds, s)
	}
	if err := rows.Err(); err != nil {
		numan_log.Error(ctx).Err(err).Msg("Erreur lors de l'itération sur les résultats SQL (rows.Err)")
		return nil, numan_error.NewInternal()
	}
	return saveds, nil
}
