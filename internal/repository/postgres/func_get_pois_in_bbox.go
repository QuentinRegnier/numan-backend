package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// FuncGetPOIsInBBox récupère les Points d'Intérêt situés dans une Bounding Box depuis PostgreSQL L3
// via la fonction SQL `content.func_get_pois_in_bbox` (fallback sur cache miss spatial L1).
func FuncGetPOIsInBBox(ctx context.Context, swLat, swLng, neLat, neLng float64, limit int) ([]map_models.POIPayload, error) {
	if postgres.ReadDB() == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	if limit <= 0 {
		limit = variables.MaxPOIGeoSearchCount
	}

	query := `
		SELECT
			id, type, title, description, latitude, longitude, address,
			phone, website, rating, comment_count, media_ids, google_maps_link, apple_maps_link, visibility, created_at, updated_at
		FROM content.func_get_pois_in_bbox($1, $2, $3, $4, $5)
	`

	rows, err := postgres.ReadDB().QueryContext(ctx, query, swLat, swLng, neLat, neLng, limit)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de rechercher les points d'intérêt dans la zone géographique.",
			err,
		)
	}
	defer func() {
		if errClose := rows.Close(); errClose != nil {
			numan_log.Error(ctx).Err(errClose).Msg("Impossible de fermer le curseur SQL pour FuncGetPOIsInBBox")
		}
	}()

	return scanPOIRows(rows)
}
