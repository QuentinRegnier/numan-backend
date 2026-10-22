package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncLoadAllUserSessions récupère les données persistantes des sessions actives d'un utilisateur.
// Les données volatiles (Ratchet Secrets, Last JWT) ne sont volontairement pas extraites du L3.
func FuncLoadAllUserSessions(ctx context.Context, userID int64) ([]auth_models.SessionsPayload, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	// On appelle la fonction SQL existante. On utilise NULL pour les filtres ignorés.
	query := `SELECT id, user_id, master_token, firebase_installation_id, device_info, ip_history, created_at, expires_at 
	          FROM auth.func_load_sessions(NULL, $1, NULL, NULL)`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les sessions de l'utilisateur.",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	sessions := make([]auth_models.SessionsPayload, 0)

	for rows.Next() {
		var s auth_models.SessionsPayload
		var devInfoBytes []byte
		var ipHistory pq.StringArray

		// On scanne uniquement les 8 colonnes renvoyées par Postgres
		err := rows.Scan(
			&s.ID, &s.UserID, &s.MasterToken, &s.FirebaseInstallationID,
			&devInfoBytes, &ipHistory, &s.CreatedAt, &s.ExpiresAt,
		)

		if err == nil {
			if len(devInfoBytes) > 0 {
				if err := json.Unmarshal(devInfoBytes, &s.DeviceInfo); err != nil {
					return nil, numan_error.NewAppError(
						http.StatusInternalServerError,
						numan_error.CodeDatabaseError,
						"Impossible de désérialiser les informations de l'appareil.",
						err,
					)
				}
			}
			s.IPHistory = ipHistory

			// Note : CurrentSecret, LastSecret, LastJWT et ToleranceTime
			// restent aux valeurs vides par défaut de Go ("" et Time{}),
			// ce qui provoquera une désynchronisation (comportement de sécurité désiré).

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
