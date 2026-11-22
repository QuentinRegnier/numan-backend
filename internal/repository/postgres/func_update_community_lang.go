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

// FuncUpdateCommunityLang met à jour atomiquement la langue d'une communauté publique et supprime son pin éventuel.
func FuncUpdateCommunityLang(ctx context.Context, convID int64, newLang string) (community_recommendation_models.LangChangeMutationResult, error) {
	if postgres.PostgresDB == nil {
		return community_recommendation_models.LangChangeMutationResult{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT status_code, old_lang, new_lang, old_pin_slot FROM messaging.func_update_community_lang($1, $2)`
	var res community_recommendation_models.LangChangeMutationResult
	var oldSlot sql.NullInt64

	if err := postgres.PostgresDB.QueryRowContext(ctx, query, convID, newLang).Scan(
		&res.StatusCode,
		&res.OldLang,
		&res.NewLang,
		&oldSlot,
	); err != nil {
		return community_recommendation_models.LangChangeMutationResult{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de mettre à jour la langue de la communauté.",
			err,
		)
	}

	if oldSlot.Valid {
		res.OldPinSlot = int(oldSlot.Int64)
	}

	return res, nil
}
