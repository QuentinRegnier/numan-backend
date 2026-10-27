package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncApplyTrustScoreDelta applique atomiquement un delta de réputation ou un bannissement majeur en L3.
func FuncApplyTrustScoreDelta(ctx context.Context, userID int64, delta float64, isInfraction bool, isMajorBan bool, banReason string) (auth_models.TrustDeltaResult, error) {
	if postgres.PostgresDB == nil {
		return auth_models.TrustDeltaResult{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}
	var reasonArg any
	if banReason != "" {
		reasonArg = banReason
	}
	const query = `SELECT new_score, is_banned FROM auth.func_apply_trust_score_delta($1, $2, $3, $4, $5)`
	var res auth_models.TrustDeltaResult
	if err := postgres.PostgresDB.QueryRowContext(ctx, query, userID, delta, isInfraction, isMajorBan, reasonArg).Scan(&res.NewScore, &res.IsBanned); err != nil {
		return auth_models.TrustDeltaResult{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de mettre à jour le score de confiance.",
			err,
		)
	}
	return res, nil
}
