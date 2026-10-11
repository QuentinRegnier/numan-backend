package postgres

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncLoadAllUserSessions récupère les données persistantes des sessions actives d'un utilisateur.
// Les données volatiles (Ratchet Secrets, Last JWT) ne sont volontairement pas extraites du L3.
func FuncLoadAllUserSessions(ctx context.Context, userID int64) ([]auth_models.SessionsPayload, error) {
	// On appelle la fonction SQL existante. On utilise NULL pour les filtres ignorés.
	query := `SELECT id, user_id, master_token, firebase_installation_id, device_info, ip_history, created_at, expires_at 
	          FROM auth.func_load_sessions(NULL, $1, NULL, NULL)`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			numan_log.Error(ctx).Err(err).Msg("Erreur fermeture rows FuncLoadAllUserSessionsPayload")
		}
	}(rows)

	var sessions []auth_models.SessionsPayload

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
				err = json.Unmarshal(devInfoBytes, &s.DeviceInfo)
				if err != nil {
					numan_log.Warn(ctx).Err(err).Msg("Erreur lors de la désérialisation des informations sur le dispositif")
					return nil, numan_error.NewInternal()
				}
			}
			s.IPHistory = ipHistory

			// Note : CurrentSecret, LastSecret, LastJWT et ToleranceTime
			// restent aux valeurs vides par défaut de Go ("" et Time{}),
			// ce qui provoquera une désynchronisation (comportement de sécurité désiré).

			sessions = append(sessions, s)
		}
	}

	return sessions, nil
}
