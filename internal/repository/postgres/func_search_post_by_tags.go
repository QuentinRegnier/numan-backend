package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncSearchPostIDsByTag appelle la procédure SQL de recherche par tag avec tri.
func FuncSearchPostIDsByTag(ctx context.Context, tag string, orderMode int, offset, limit int64, lang ...int) ([]int64, error) {
	if postgres.ReadDB() == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	var (
		query string
		args  []any
	)
	if len(lang) > 0 && lang[0] >= 0 {
		query = `SELECT id FROM content.func_search_post_ids_by_tag($1, $2::smallint, $3::integer, $4::integer, $5::smallint)`
		args = []any{tag, orderMode, offset, limit, lang[0]}
	} else {
		query = `SELECT id FROM content.func_search_post_ids_by_tag($1, $2::smallint, $3::integer, $4::integer)`
		args = []any{tag, orderMode, offset, limit}
	}
	rows, err := postgres.ReadDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de rechercher les publications par tag.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

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
