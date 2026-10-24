package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncLoadUserSessionsView récupère la vue expurgée des sessions (Pur DDD)
func FuncLoadUserSessionsView(ctx context.Context, userID int64) ([]auth_models.SessionView, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	// Appel direct à la nouvelle fonction SQL dédiée
	query := `SELECT id, device_info, ip_history, created_at, expires_at 
	          FROM auth.func_get_active_sessions_view($1)`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les sessions de l'utilisateur.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	sessions := make([]auth_models.SessionView, 0)
	for rows.Next() {
		var s auth_models.SessionView
		var deviceInfoBytes []byte

		if err := rows.Scan(&s.ID, &deviceInfoBytes, pq.Array(&s.IPHistory), &s.CreatedAt, &s.ExpiresAt); err == nil {
			if len(deviceInfoBytes) > 0 {
				if err := json.Unmarshal(deviceInfoBytes, &s.DeviceInfo); err != nil {
					return nil, numan_error.NewAppError(
						http.StatusInternalServerError,
						numan_error.CodeDatabaseError,
						"Impossible de désérialiser les informations de l'appareil.",
						err,
					)
				}
			}
			sessions = append(sessions, s)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les sessions de l'utilisateur.",
			err,
		)
	}

	return sessions, nil
}
