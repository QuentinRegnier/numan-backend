package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncLoadActiveCommunities appelle la fonction SQL pour récupérer les communautés publiques
func FuncLoadActiveCommunities(ctx context.Context) ([]lite_models.CommunityLiteRequest, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT id, name, description, avatar_id, member_count FROM messaging.func_load_active_communities()`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les communautés actives.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	communities := make([]lite_models.CommunityLiteRequest, 0)
	for rows.Next() {
		var c lite_models.CommunityLiteRequest
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.ProfilePictureID, &c.MemberCount); err == nil {
			communities = append(communities, c)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les communautés actives.",
			err,
		)
	}

	return communities, nil
}
