package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncIncrementMonthlyTrustScores exécute la récompense mensuelle (+0.02) pour les comptes sans infraction depuis 30 jours.
func FuncIncrementMonthlyTrustScores(ctx context.Context) ([]auth_models.MonthlyTrustReward, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}
	const query = `SELECT user_id, new_trust_score FROM auth.func_increment_monthly_trust_scores()`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'exécuter l'incrémentation mensuelle des scores de confiance.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	rewards := make([]auth_models.MonthlyTrustReward, 0)
	for rows.Next() {
		var r auth_models.MonthlyTrustReward
		if err := rows.Scan(&r.UserID, &r.NewTrustScore); err == nil {
			rewards = append(rewards, r)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les récompenses mensuelles de confiance.",
			err,
		)
	}
	return rewards, nil
}
