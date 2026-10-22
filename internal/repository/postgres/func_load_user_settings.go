package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncLoadUserSettings charge les paramètres complets d'un utilisateur (incluant son ADN algorithmique)
func FuncLoadUserSettings(ctx context.Context, userID int64) (user_settings_models.UserSettingsPayload, error) {
	if postgres.PostgresDB == nil {
		return user_settings_models.UserSettingsPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT id, user_id, privacy, notifications, display_and_content, telemetry_vector, telemetry_tags, telemetry_timestamp
	          FROM auth.func_load_user_settings(NULL, $1)`

	var s user_settings_models.UserSettingsPayload
	var privacyBytes, notifBytes, displayBytes []byte

	err := postgres.PostgresDB.QueryRowContext(ctx, query, userID).Scan(
		&s.ID,
		&s.UserID,
		&privacyBytes,
		&notifBytes,
		&displayBytes,
		pq.Array(&s.TelemetryVector),
		pq.Array(&s.TelemetryTags),
		&s.TelemetryTimestamp,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Ce n'est pas une erreur, l'utilisateur n'a simplement pas encore de settings
			return s, nil
		}
		return user_settings_models.UserSettingsPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les paramètres de l'utilisateur.",
			err,
		)
	}

	// Conversion des JSONB en Map
	if len(privacyBytes) > 0 {
		if err := json.Unmarshal(privacyBytes, &s.Privacy); err != nil {
			return user_settings_models.UserSettingsPayload{}, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de désérialiser les paramètres de confidentialité.",
				err,
			)
		}
	}
	if len(notifBytes) > 0 {
		if err := json.Unmarshal(notifBytes, &s.Notifications); err != nil {
			return user_settings_models.UserSettingsPayload{}, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de désérialiser les paramètres de notification.",
				err,
			)
		}
	}
	if len(displayBytes) > 0 {
		if err := json.Unmarshal(displayBytes, &s.DisplayAndContent); err != nil {
			return user_settings_models.UserSettingsPayload{}, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de désérialiser les paramètres d'affichage et de contenu.",
				err,
			)
		}
	}

	return s, nil
}
