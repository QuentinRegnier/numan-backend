package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

func FuncLoadRelationsByDirectionPaginated(ctx context.Context, userID int64, state int, direction string, limit int64, offset int64) ([]int64, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT matched_id FROM auth.func_load_relations_paginated_by_direction($1, $2::smallint, $3, $4, $5)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, userID, state, direction, limit, offset)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les relations par direction.",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	targetIDs := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			targetIDs = append(targetIDs, id)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les relations par direction.",
			err,
		)
	}

	return targetIDs, nil
}
