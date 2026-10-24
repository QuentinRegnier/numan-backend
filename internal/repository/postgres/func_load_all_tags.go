package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncLoadAllTags récupère tous les slugs actifs depuis la base de données.
func FuncLoadAllTags(ctx context.Context) ([]string, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	sqlStatement := `SELECT slug FROM content.func_load_all_tags()`

	rows, err := postgres.PostgresDB.QueryContext(ctx, sqlStatement)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les tags.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	tags := make([]string, 0)
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err == nil {
			tags = append(tags, slug)
		}
	}

	if err = rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les tags.",
			err,
		)
	}

	return tags, nil
}
