package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

func FuncLoadActiveConversations(ctx context.Context) ([]lite_models.ConvLiteRequest, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT id, type, title, description, avatar_id, last_message_id, settings, external_link FROM messaging.func_load_active_conversations()`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les conversations actives.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	results := make([]lite_models.ConvLiteRequest, 0)
	for rows.Next() {
		var cid int64
		var cType int
		var title sql.NullString
		var lastMsgID sql.NullInt64
		var description sql.NullString
		var avatarID sql.NullInt64
		var settingsRaw sql.NullString
		var externalLinksRaw sql.NullString

		if err := rows.Scan(&cid, &cType, &title, &description, &avatarID, &lastMsgID, &settingsRaw, &externalLinksRaw); err == nil {
			meta := lite_models.ConvLiteRequest{ID: cid, Type: cType}
			if title.Valid {
				meta.Title = title.String
			}
			if description.Valid {
				meta.Description = description.String
			}
			if avatarID.Valid {
				meta.AvatarID = avatarID.Int64
			}
			if lastMsgID.Valid {
				meta.LastMessageID = lastMsgID.Int64
			}
			if settingsRaw.Valid && settingsRaw.String != "" && settingsRaw.String != "{}" {
				if err := json.Unmarshal([]byte(settingsRaw.String), &meta.Settings); err != nil {
					return nil, numan_error.NewAppError(
						http.StatusInternalServerError,
						numan_error.CodeDatabaseError,
						"Impossible de désérialiser les paramètres de la conversation.",
						err,
					)
				}
			}
			if externalLinksRaw.Valid && externalLinksRaw.String != "" {
				if err := json.Unmarshal([]byte(externalLinksRaw.String), &meta.ExternalLink); err != nil {
					return nil, numan_error.NewAppError(
						http.StatusInternalServerError,
						numan_error.CodeDatabaseError,
						"Impossible de désérialiser les liens externes de la conversation.",
						err,
					)
				}
			}
			results = append(results, meta)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les conversations actives.",
			err,
		)
	}

	return results, nil
}
