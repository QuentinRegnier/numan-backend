package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

func FuncCheckUnique(ctx context.Context, entity redis.EntityType, field string, value string) (bool, error) {
	target, err := redis2Postgres(ctx, entity)
	if err != nil {
		return false, err
	}

	if postgres.PostgresDB == nil {
		return false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT views.func_check_unique($1, $2, $3, $4::text)`

	var exists bool
	err = postgres.PostgresDB.QueryRowContext(ctx, query, target.Schema, target.Table, field, value).Scan(&exists)
	if err != nil {
		return false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de vérifier l'unicité de la ressource.",
			err,
		)
	}

	return exists, nil
}
