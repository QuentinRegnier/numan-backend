package postgres

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncGetPOIsByIDs récupère un lot de Points d'Intérêt depuis PostgreSQL L3 via `content.func_get_pois_by_ids`.
func FuncGetPOIsByIDs(ctx context.Context, poiIDs []int64) ([]map_models.POIPayload, error) {
	if len(poiIDs) == 0 {
		return []map_models.POIPayload{}, nil
	}

	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `
		SELECT
			id, type, title, description, latitude, longitude, address,
			phone, website, rating, comment_count, media_ids, google_maps_link, apple_maps_link, visibility, created_at, updated_at
		FROM content.func_get_pois_by_ids($1)
	`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query, pq.Array(poiIDs))
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les points d'intérêt par identifiants.",
			err,
		)
	}
	defer func() {
		if errClose := rows.Close(); errClose != nil {
			numan_log.Error(ctx).Err(errClose).Msg("Impossible de fermer le curseur SQL pour FuncGetPOIsByIDs")
		}
	}()

	return scanPOIRows(rows)
}

func scanPOIRows(rows *sql.Rows) ([]map_models.POIPayload, error) {
	pois := make([]map_models.POIPayload, 0)
	for rows.Next() {
		var p map_models.POIPayload
		var phoneSQL, websiteSQL, googleLinkSQL, appleLinkSQL sql.NullString
		var mediaIDs pq.Int64Array

		if err := rows.Scan(
			&p.ID,
			&p.Type,
			&p.Title,
			&p.Description,
			&p.Latitude,
			&p.Longitude,
			&p.Address,
			&phoneSQL,
			&websiteSQL,
			&p.Rating,
			&p.CommentCount,
			&mediaIDs,
			&googleLinkSQL,
			&appleLinkSQL,
			&p.Visibility,
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de décoder les données du point d'intérêt.",
				err,
			)
		}

		if phoneSQL.Valid {
			p.Phone = phoneSQL.String
		}
		if websiteSQL.Valid {
			p.Website = websiteSQL.String
		}
		if googleLinkSQL.Valid {
			p.GoogleMapsLink = googleLinkSQL.String
		}
		if appleLinkSQL.Valid {
			p.AppleMapsLink = appleLinkSQL.String
		}
		if len(mediaIDs) > 0 {
			p.MediaIDs = []int64(mediaIDs)
		} else {
			p.MediaIDs = []int64{}
		}
		pois = append(pois, p)
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Erreur lors du parcours des résultats des points d'intérêt.",
			err,
		)
	}

	return pois, nil
}
