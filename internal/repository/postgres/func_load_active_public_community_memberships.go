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

// FuncLoadActivePublicCommunityMemberships charge par lots les appartenances actives aux communautés publiques.
func FuncLoadActivePublicCommunityMemberships(ctx context.Context, limit int, offset int) ([]community_recommendation_models.PublicCommunityMembershipRecord, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT user_id, conversation_id FROM messaging.func_load_active_public_community_memberships($1, $2)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les appartenances aux communautés publiques.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL")
		}
	}()

	results := make([]community_recommendation_models.PublicCommunityMembershipRecord, 0)
	for rows.Next() {
		var rec community_recommendation_models.PublicCommunityMembershipRecord
		if err := rows.Scan(&rec.UserID, &rec.ConversationID); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de lire une appartenance communautaire.",
				err,
			)
		}
		results = append(results, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Erreur d'itération sur les appartenances communautaires.",
			err,
		)
	}

	return results, nil
}
