package postgres

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

func FuncCheckUnique(ctx context.Context, entity redis.EntityType, field string, value string) (bool, error) {
	target, err := redis2Postgres(ctx, entity)
	if err != nil {
		numan_log.Error(ctx).Err(err).Str("entity", string(entity)).Msg("Échec de la résolution de la table PostgreSQL depuis l'entité Redis")
		return false, numan_error.NewInternal() // Coupe-circuit immédiat
	}

	query := `SELECT views.func_check_unique($1, $2, $3, $4::text)`

	var exists bool

	err = postgres.PostgresDB.QueryRowContext(ctx, query, target.Schema, target.Table, field, value).Scan(&exists)

	return exists, err
}
