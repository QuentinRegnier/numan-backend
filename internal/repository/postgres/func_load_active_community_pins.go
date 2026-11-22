package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncLoadActiveCommunityPins charge tous les pins actifs des communautés publiques pour le seeding L1.
func FuncLoadActiveCommunityPins(ctx context.Context) ([]community_recommendation_models.CommunityPinRecord, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT conversation_id, lang, pin_slot FROM messaging.func_load_active_community_pins()`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les communautés épinglées.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL")
		}
	}()

	results := make([]community_recommendation_models.CommunityPinRecord, 0)
	for rows.Next() {
		var rec community_recommendation_models.CommunityPinRecord
		if err := rows.Scan(&rec.ConversationID, &rec.Lang, &rec.PinSlot); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de lire une communauté épinglée.",
				err,
			)
		}
		results = append(results, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Erreur d'itération sur les communautés épinglées.",
			err,
		)
	}

	return results, nil
}
