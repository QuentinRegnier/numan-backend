package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/saved_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncLoadSavedPostsKeyset charge les publications sauvegardées via content.func_load_saved_posts_keyset.
func FuncLoadSavedPostsKeyset(ctx context.Context, userID int64, beforeCreatedAt *int64, beforeID *int64, limit int64) ([]saved_models.SavedPayload, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT id, user_id, post_id, created_at FROM content.func_load_saved_posts_keyset($1, $2, $3, $4)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, userID, beforeCreatedAt, beforeID, limit)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les publications sauvegardées.",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	saveds := make([]saved_models.SavedPayload, 0)
	for rows.Next() {
		var s saved_models.SavedPayload
		if err := rows.Scan(&s.ID, &s.UserID, &s.PostID, &s.CreatedAt); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de scanner les données du post sauvegardé.",
				err,
			)
		}
		saveds = append(saveds, s)
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les publications sauvegardées.",
			err,
		)
	}

	return saveds, nil
}
