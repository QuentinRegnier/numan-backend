package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncLoadCommunityMembersForTrust charge en un seul appel batch les owners, admins et un échantillon
// borné de membres normaux pour un ensemble de communautés publiques.
func FuncLoadCommunityMembersForTrust(ctx context.Context, convIDs []int64, memberCap int) ([]community_recommendation_models.CommunityMemberRoleRecord, error) {
	if len(convIDs) == 0 {
		return []community_recommendation_models.CommunityMemberRoleRecord{}, nil
	}
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT conversation_id, user_id, role FROM messaging.func_load_community_members_for_trust($1, $2)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, pq.Array(convIDs), memberCap)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les rôles des membres des communautés.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL")
		}
	}()

	results := make([]community_recommendation_models.CommunityMemberRoleRecord, 0)
	for rows.Next() {
		var rec community_recommendation_models.CommunityMemberRoleRecord
		if err := rows.Scan(&rec.ConversationID, &rec.UserID, &rec.Role); err != nil {
			return nil, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de lire un rôle de membre.",
				err,
			)
		}
		results = append(results, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Erreur d'itération sur les rôles des membres.",
			err,
		)
	}

	return results, nil
}
