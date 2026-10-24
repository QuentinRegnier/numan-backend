package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncLoadUserPostsKeyset charge les publications d'un utilisateur via content.func_load_user_posts_keyset.
// Le curseur est composé de (beforeCreatedAt, beforeID). Si les deux sont nil, la première page est renvoyée.
func FuncLoadUserPostsKeyset(ctx context.Context, userID int64, beforeCreatedAt *int64, beforeID *int64, limit int64) ([]post_models.PostPayload, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT * FROM content.func_load_user_posts_keyset($1, $2, $3, $4)`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query, userID, beforeCreatedAt, beforeID, limit)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les publications de l'utilisateur.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	return scanPosts(ctx, rows)
}
