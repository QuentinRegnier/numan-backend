package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncDeleteOrphanSaved appelle la fonction SQL pour purger les favoris orphelins
// et retourne la liste des post_ids concernés.
func FuncDeleteOrphanSaved(ctx context.Context) ([]int64, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT post_id FROM content.func_delete_orphan_saved()`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'exécuter la purge des favoris orphelins.",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	orphanPostIDs := make([]int64, 0)
	for rows.Next() {
		var postID int64
		if err := rows.Scan(&postID); err == nil {
			orphanPostIDs = append(orphanPostIDs, postID)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les favoris orphelins.",
			err,
		)
	}

	return orphanPostIDs, nil
}
