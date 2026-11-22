package postgres

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncUnsetCommunityPin retire de manière idempotente le pin d'une communauté dans PostgreSQL (L3).
func FuncUnsetCommunityPin(ctx context.Context, convID int64) (community_recommendation_models.UnpinMutationResult, error) {
	if postgres.PostgresDB == nil {
		return community_recommendation_models.UnpinMutationResult{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT status_code, conv_lang, old_pin_slot FROM messaging.func_unset_community_pin($1)`
	var res community_recommendation_models.UnpinMutationResult
	var oldSlot sql.NullInt64

	if err := postgres.PostgresDB.QueryRowContext(ctx, query, convID).Scan(
		&res.StatusCode,
		&res.ConvLang,
		&oldSlot,
	); err != nil {
		return community_recommendation_models.UnpinMutationResult{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de désépingler la communauté.",
			err,
		)
	}

	if oldSlot.Valid {
		res.OldPinSlot = int(oldSlot.Int64)
	}

	return res, nil
}
