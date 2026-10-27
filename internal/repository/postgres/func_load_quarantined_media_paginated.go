package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncLoadQuarantinedMediaPaginated charge la file des médias en quarantaine (visibility = 2) pour le back-office.
func FuncLoadQuarantinedMediaPaginated(ctx context.Context, limit, offset int64) ([]media_models.MediaPayload, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}
	const query = `SELECT id, owner_id, storage_path, visibility, created_at, updated_at FROM moderation.func_load_quarantined_media_paginated($1::integer, $2::integer)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les médias en quarantaine.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	items := make([]media_models.MediaPayload, 0)
	for rows.Next() {
		var m media_models.MediaPayload
		if err := rows.Scan(&m.ID, &m.OwnerID, &m.StoragePath, &m.Visibility, &m.CreatedAt, &m.UpdatedAt); err == nil {
			items = append(items, m)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les médias en quarantaine.",
			err,
		)
	}
	return items, nil
}
