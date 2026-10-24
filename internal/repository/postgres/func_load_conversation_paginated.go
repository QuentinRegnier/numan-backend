package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

type fullInboxResult struct {
	Conversation conversation_models.ConversationPayload
	Member       member_models.MemberPayload
}

func FuncLoadConversationPaginated(ctx context.Context, userID int64, limit int64, offset int64) ([]fullInboxResult, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT conv_id, conv_type, conv_title, conv_description, conv_avatar_id, conv_last_msg_id, conv_state, conv_settings, external_link, conv_created, conv_updated, mem_id, mem_conv_id, mem_user_id, mem_role, mem_settings, mem_joined, mem_unread, mem_frozen_id, mem_last_read_message_id, mem_created, mem_updated FROM messaging.func_load_conversation_paginated($1, $2, $3)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les conversations.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	results := make([]fullInboxResult, 0)
	for rows.Next() {
		var c conversation_models.ConversationPayload
		var m member_models.MemberPayload
		var cTitle, cDescription sql.NullString
		var cAvatarID, cLastMsgID sql.NullInt64
		var convSettingsRaw, memSettingsRaw, externalLink sql.NullString
		var memFrozenID sql.NullInt64
		var memLastID sql.NullInt64

		err := rows.Scan(
			&c.ID, &c.Type, &cTitle, &cDescription, &cAvatarID, &cLastMsgID, &c.State, &convSettingsRaw, &externalLink, &c.CreatedAt, &c.UpdatedAt,
			&m.ID, &m.ConversationID, &m.UserID, &m.Role, &memSettingsRaw, &m.JoinedAt, &m.UnreadCount, &memFrozenID, &memLastID, &m.CreatedAt, &m.UpdatedAt,
		)
		if err == nil {
			if cTitle.Valid {
				c.Title = cTitle.String
			}
			if cDescription.Valid {
				c.Description = cDescription.String
			}
			if cAvatarID.Valid {
				c.AvatarID = cAvatarID.Int64
			}
			if cLastMsgID.Valid {
				c.LastMessageID = cLastMsgID.Int64
			}
			if memFrozenID.Valid {
				m.FrozenMessageID = memFrozenID.Int64
			}
			if memLastID.Valid {
				m.LastReadMessageID = memLastID.Int64
			}

			if convSettingsRaw.Valid && convSettingsRaw.String != "" && convSettingsRaw.String != "{}" {
				if err := json.Unmarshal([]byte(convSettingsRaw.String), &c.Settings); err != nil {
					return nil, numan_error.NewAppError(
						http.StatusInternalServerError,
						numan_error.CodeDatabaseError,
						"Impossible de désérialiser les paramètres de la conversation.",
						err,
					)
				}
			}
			if externalLink.Valid && externalLink.String != "" && externalLink.String != "{}" {
				if err := json.Unmarshal([]byte(externalLink.String), &c.ExternalLink); err != nil {
					return nil, numan_error.NewAppError(
						http.StatusInternalServerError,
						numan_error.CodeDatabaseError,
						"Impossible de désérialiser les liens externes de la conversation.",
						err,
					)
				}
			}
			if memSettingsRaw.Valid && memSettingsRaw.String != "" && memSettingsRaw.String != "{}" {
				if err := json.Unmarshal([]byte(memSettingsRaw.String), &m.Settings); err != nil {
					return nil, numan_error.NewAppError(
						http.StatusInternalServerError,
						numan_error.CodeDatabaseError,
						"Impossible de désérialiser les paramètres du membre.",
						err,
					)
				}
			}

			results = append(results, fullInboxResult{Conversation: c, Member: m})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les conversations.",
			err,
		)
	}

	return results, nil
}
