package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncAddIndirectTagToPost appelle directement la fonction SQL compilée (Pur DDD)
// pour ajouter un tag indirect sans risque de Race Condition.
func FuncAddIndirectTagToPost(ctx context.Context, postID int64, tag string) error {
	if postgres.PostgresDB == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT content.func_add_indirect_tag_to_post($1, $2)`

	_, err := postgres.PostgresDB.ExecContext(ctx, query, postID, tag)
	if err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'ajouter le tag indirect au post.",
			err,
		)
	}

	return nil
}
