package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

func FuncGetMedia(ctx context.Context, mediaID int64) (media_models.MediaPayload, error) {
	if postgres.PostgresDB == nil {
		return media_models.MediaPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT id, owner_id, storage_path, visibility, created_at, updated_at FROM content.get_media($1)`

	var m media_models.MediaPayload
	err := postgres.PostgresDB.QueryRowContext(ctx, query, mediaID).Scan(
		&m.ID,
		&m.OwnerID,
		&m.StoragePath,
		&m.Visibility,
		&m.CreatedAt,
		&m.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return media_models.MediaPayload{}, numan_error.NewNotFound(
				"MEDIA_NOT_FOUND",
				"Média introuvable.",
				fmt.Errorf("media %d was not found in PostgreSQL (L3)", mediaID),
			)
		}
		return media_models.MediaPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer le média.",
			err,
		)
	}

	// 🛑 Rejet si le média a été supprimé (Soft-Delete = -1) ou rejeté par la modération (-2)
	if m.Visibility == -1 {
		return m, numan_error.NewNotFound(
			"MEDIA_DELETED",
			"Ce média a été supprimé.",
			errors.New("media is soft-deleted"),
		)
	}
	if m.Visibility == -2 {
		return m, numan_error.NewForbidden(
			numan_error.CodeMediaRejected,
			"Ce média a été retiré suite à une infraction aux règles de la communauté.",
			errors.New("media was rejected by moderation"),
		)
	}

	return m, nil
}
