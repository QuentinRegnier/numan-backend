package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

type mutedMemberRecord struct {
	UserID          int64
	RestrictedUntil int64
}

// FuncLoadMutedMembersPaginated appelle la fonction SQL pour lister les membres restreints
func FuncLoadMutedMembersPaginated(ctx context.Context, convID int64, limit int64, offset int64) ([]mutedMemberRecord, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	rows, err := postgres.PostgresDB.QueryContext(ctx, "SELECT * FROM messaging.func_load_muted_members_paginated($1, $2, $3)", convID, limit, offset)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les membres restreints.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	records := make([]mutedMemberRecord, 0)
	for rows.Next() {
		var r mutedMemberRecord
		if err := rows.Scan(&r.UserID, &r.RestrictedUntil); err == nil {
			records = append(records, r)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les membres restreints.",
			err,
		)
	}

	return records, nil
}
