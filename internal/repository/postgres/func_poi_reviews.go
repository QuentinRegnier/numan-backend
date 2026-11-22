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

// FuncGetPOIReviewByUser récupère l'avis unique (note avec ou sans texte) d'un utilisateur pour un POI depuis PostgreSQL L3.
func FuncGetPOIReviewByUser(ctx context.Context, poiID, userID int64) (map_models.POIReviewPayload, bool, error) {
	if poiID <= 0 || userID <= 0 {
		return map_models.POIReviewPayload{}, false, nil
	}

	if postgres.ReadDB() == nil {
		return map_models.POIReviewPayload{}, false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `
		SELECT id, poi_id, user_id, rating, content, visibility, created_at, updated_at
		FROM content.func_get_poi_review_by_user($1, $2)
	`

	var r map_models.POIReviewPayload
	var contentSQL sql.NullString
	err := postgres.ReadDB().QueryRowContext(ctx, query, poiID, userID).Scan(
		&r.ID,
		&r.POIID,
		&r.UserID,
		&r.Rating,
		&contentSQL,
		&r.Visibility,
		&r.CreatedAt,
		&r.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return map_models.POIReviewPayload{}, false, nil
		}
		return map_models.POIReviewPayload{}, false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger l'avis utilisateur sur le point d'intérêt.",
			err,
		)
	}

	if contentSQL.Valid {
		r.Content = contentSQL.String
	}
	return r, true, nil
}

// FuncGetPOIReviewsPaginated récupère les avis textuels actifs (content != "") paginés d'un POI depuis PostgreSQL L3.
func FuncGetPOIReviewsPaginated(ctx context.Context, poiID int64, limit, offset int) ([]map_models.POIReviewPayload, error) {
	if poiID <= 0 {
		return []map_models.POIReviewPayload{}, nil
	}

	if postgres.ReadDB() == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `
		SELECT id, poi_id, user_id, rating, content, visibility, created_at, updated_at
		FROM content.func_get_poi_reviews_paginated($1, $2, $3)
	`

	rows, err := postgres.ReadDB().QueryContext(ctx, query, poiID, limit, offset)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger la liste des avis du point d'intérêt.",
			err,
		)
	}
	defer func() {
		if errClose := rows.Close(); errClose != nil {
			numan_log.Error(ctx).Err(errClose).Msg("Impossible de fermer le curseur SQL pour FuncGetPOIReviewsPaginated")
		}
	}()

	return scanPOIReviewRows(rows)
}

// FuncGetPOIReviewsByIDs récupère un lot d'avis textuels actifs par identifiants depuis PostgreSQL L3 (auto-guérison L3 -> L1).
func FuncGetPOIReviewsByIDs(ctx context.Context, reviewIDs []int64) ([]map_models.POIReviewPayload, error) {
	if len(reviewIDs) == 0 {
		return []map_models.POIReviewPayload{}, nil
	}

	if postgres.ReadDB() == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `
		SELECT id, poi_id, user_id, rating, content, visibility, created_at, updated_at
		FROM content.func_get_poi_reviews_by_ids($1)
	`

	rows, err := postgres.ReadDB().QueryContext(ctx, query, pq.Array(reviewIDs))
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les avis du point d'intérêt par identifiants.",
			err,
		)
	}
	defer func() {
		if errClose := rows.Close(); errClose != nil {
			numan_log.Error(ctx).Err(errClose).Msg("Impossible de fermer le curseur SQL pour FuncGetPOIReviewsByIDs")
		}
	}()

	return scanPOIReviewRows(rows)
}

func scanPOIReviewRows(rows *sql.Rows) ([]map_models.POIReviewPayload, error) {
	reviews := make([]map_models.POIReviewPayload, 0)
	for rows.Next() {
		var r map_models.POIReviewPayload
		var contentSQL sql.NullString

		if err := rows.Scan(
			&r.ID,
			&r.POIID,
			&r.UserID,
			&r.Rating,
			&contentSQL,
			&r.Visibility,
			&r.CreatedAt,
			&r.UpdatedAt,
		); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de décoder les données de l'avis POI.",
				err,
			)
		}
		if contentSQL.Valid {
			r.Content = contentSQL.String
		}
		reviews = append(reviews, r)
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Erreur lors du parcours des résultats des avis POI.",
			err,
		)
	}

	return reviews, nil
}
