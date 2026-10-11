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

// FuncLoadUserSessionsView récupère la vue expurgée des sessions (Pur DDD)
func FuncLoadUserSessionsView(ctx context.Context, userID int64) ([]auth_models.SessionView, error) {
	// Appel direct à la nouvelle fonction SQL dédiée
	query := `SELECT id, device_info, ip_history, created_at, expires_at 
	          FROM auth.func_get_active_sessions_view($1)`

	rows, err := postgres.PostgresDB.QueryContext(ctx, query, userID)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Échec de l'exécution de la requête SQL (QueryContext)")
		return nil, numan_error.NewInternal()
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			numan_log.Error(ctx).Err(err).Msg("Erreur lors de la fermeture des lignes Postgres")
		}
	}(rows)

	var sessions []auth_models.SessionView
	for rows.Next() {
		var s auth_models.SessionView
		var deviceInfoBytes []byte

		if err := rows.Scan(&s.ID, &deviceInfoBytes, pq.Array(&s.IPHistory), &s.CreatedAt, &s.ExpiresAt); err == nil {
			err = json.Unmarshal(deviceInfoBytes, &s.DeviceInfo)
			if err != nil {
				numan_log.Warn(ctx).Err(err).Msg("Erreur lors de la désérialisation des informations du dispositif")
				return nil, numan_error.NewInternal()
			}
			sessions = append(sessions, s)
		}
	}
	return sessions, nil
}
