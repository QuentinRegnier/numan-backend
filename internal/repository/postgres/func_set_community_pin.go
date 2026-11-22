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

// FuncSetCommunityPin exécute l'épinglage atomique dans PostgreSQL (L3) avec gestion de collision.
func FuncSetCommunityPin(ctx context.Context, convID int64, slot int, forceReplace bool) (community_recommendation_models.PinMutationResult, error) {
	if postgres.PostgresDB == nil {
		return community_recommendation_models.PinMutationResult{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT status_code, conv_lang, old_pin_slot, displaced_conv_id FROM messaging.func_set_community_pin($1, $2, $3)`
	var res community_recommendation_models.PinMutationResult
	var oldSlot sql.NullInt64
	var displaced sql.NullInt64

	if err := postgres.PostgresDB.QueryRowContext(ctx, query, convID, slot, forceReplace).Scan(
		&res.StatusCode,
		&res.ConvLang,
		&oldSlot,
		&displaced,
	); err != nil {
		return community_recommendation_models.PinMutationResult{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'épingler la communauté.",
			err,
		)
	}

	if oldSlot.Valid {
		res.OldPinSlot = int(oldSlot.Int64)
	}
	if displaced.Valid {
		res.DisplacedConversationID = displaced.Int64
	}

	return res, nil
}
