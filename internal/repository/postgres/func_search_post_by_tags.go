package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncSearchPostIDsByTag appelle la procédure SQL de recherche par tag avec tri.
func FuncSearchPostIDsByTag(ctx context.Context, tag string, orderMode int, offset, limit int64) ([]int64, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT id FROM content.func_search_post_ids_by_tag($1, $2::smallint, $3::integer, $4::integer)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, tag, orderMode, offset, limit)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de rechercher les publications par tag.",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur la recherche des publications par tag.",
			err,
		)
	}

	return ids, nil
}
