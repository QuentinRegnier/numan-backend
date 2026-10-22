package postgres

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// timelineSeedPayload structure temporaire pour la reconstruction L1
type timelineSeedPayload struct {
	PostID    int64
	UserID    int64
	CreatedAt time.Time
}

// FuncLoadTimelineSeedPaginated appelle la fonction SQL content.func_load_timeline_seed_paginated
func FuncLoadTimelineSeedPaginated(ctx context.Context, limit, offset int) ([]timelineSeedPayload, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT * FROM content.func_load_timeline_seed_paginated($1, $2)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les données d'amorçage de la timeline.",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	seeds := make([]timelineSeedPayload, 0)
	for rows.Next() {
		var s timelineSeedPayload
		if err := rows.Scan(&s.PostID, &s.UserID, &s.CreatedAt); err == nil {
			seeds = append(seeds, s)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les données d'amorçage de la timeline.",
			err,
		)
	}

	return seeds, nil
}
