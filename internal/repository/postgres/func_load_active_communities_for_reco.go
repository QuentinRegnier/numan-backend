package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncLoadActiveCommunitiesForReco charge toutes les communautés publiques actives (type=3, state=0)
// avec leurs métadonnées complètes pour le worker de scoring.
func FuncLoadActiveCommunitiesForReco(ctx context.Context) ([]community_recommendation_models.ActiveCommunityRecoRecord, error) {
	if postgres.ReadDB() == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT id, title, description, avatar_id, lang, is_partner, pin_slot, settings, created_at, member_count FROM messaging.func_load_active_communities_for_reco()`
	rows, err := postgres.ReadDB().QueryContext(ctx, query)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les communautés publiques actives.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL")
		}
	}()

	results := make([]community_recommendation_models.ActiveCommunityRecoRecord, 0)
	for rows.Next() {
		var rec community_recommendation_models.ActiveCommunityRecoRecord
		var pinSlot sql.NullInt64
		var settingsRaw sql.NullString

		if err := rows.Scan(
			&rec.ID,
			&rec.Title,
			&rec.Description,
			&rec.AvatarID,
			&rec.Lang,
			&rec.IsPartner,
			&pinSlot,
			&settingsRaw,
			&rec.CreatedAt,
			&rec.MemberCount,
		); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de lire une communauté publique active.",
				err,
			)
		}

		if pinSlot.Valid {
			rec.PinSlot = int(pinSlot.Int64)
		}
		if settingsRaw.Valid && settingsRaw.String != "" && settingsRaw.String != "{}" {
			if err := json.Unmarshal([]byte(settingsRaw.String), &rec.Settings); err != nil {
				return nil, numan_error.NewAppError(
					http.StatusInternalServerError,
					numan_error.CodeDatabaseError,
					"Impossible de désérialiser les paramètres d'une communauté.",
					err,
				)
			}
		}
		results = append(results, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Erreur d'itération lors du chargement des communautés actives.",
			err,
		)
	}

	return results, nil
}
