package postgres

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// relationSeedPayload structure temporaire pour l'amorçage
type relationSeedPayload struct {
	CallerID  int64
	TargetID  int64
	State     int
	CreatedAt time.Time
}

// FuncLoadRelationsPaginated appelle la fonction SQL auth.func_load_relations_paginated
func FuncLoadRelationsPaginated(ctx context.Context, limit, offset int) ([]relationSeedPayload, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT * FROM auth.func_load_relations_paginated($1, $2)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les relations paginées.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	relations := make([]relationSeedPayload, 0)
	for rows.Next() {
		var r relationSeedPayload
		if err := rows.Scan(&r.CallerID, &r.TargetID, &r.State, &r.CreatedAt); err == nil {
			relations = append(relations, r)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les relations paginées.",
			err,
		)
	}

	return relations, nil
}
