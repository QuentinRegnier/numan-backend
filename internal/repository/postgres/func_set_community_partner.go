package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncSetCommunityPartner met à jour le statut partenaire d'une communauté publique dans PostgreSQL (L3).
func FuncSetCommunityPartner(ctx context.Context, convID int64, isPartner bool) (community_recommendation_models.PartnerMutationResult, error) {
	if postgres.PostgresDB == nil {
		return community_recommendation_models.PartnerMutationResult{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT status_code, conv_lang, is_partner FROM messaging.func_set_community_partner($1, $2)`
	var res community_recommendation_models.PartnerMutationResult

	if err := postgres.PostgresDB.QueryRowContext(ctx, query, convID, isPartner).Scan(
		&res.StatusCode,
		&res.ConvLang,
		&res.IsPartner,
	); err != nil {
		return community_recommendation_models.PartnerMutationResult{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de modifier le statut partenaire de la communauté.",
			err,
		)
	}

	return res, nil
}
