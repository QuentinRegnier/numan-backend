package postgres

import (
	"context"
	"database/sql"
	"encoding/json"

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
	query := `SELECT conv_id, conv_type, conv_title, conv_description, conv_avatar_id, conv_last_msg_id, conv_state, conv_settings, external_link, conv_created, conv_updated, mem_id, mem_conv_id, mem_user_id, mem_role, mem_settings, mem_joined, mem_unread, mem_frozen_id, mem_last_read_message_id, mem_created, mem_updated FROM messaging.func_load_conversation_paginated($1, $2, $3)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, userID, limit, offset)
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

	var results []fullInboxResult
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
				err = json.Unmarshal([]byte(convSettingsRaw.String), &c.Settings)
				if err != nil {
					numan_log.Warn(ctx).Err(err).Msg("Erreur lors de la désérialisation des paramètres de conversation")
					return nil, numan_error.NewInternal()
				}
			}
			if externalLink.Valid && externalLink.String != "" && externalLink.String != "{}" {
				err = json.Unmarshal([]byte(externalLink.String), &c.ExternalLink)
				if err != nil {
					numan_log.Warn(ctx).Err(err).Msg("Erreur lors de la désérialisation des liens externes")
					return nil, numan_error.NewInternal()
				}
			}
			if memSettingsRaw.Valid && memSettingsRaw.String != "" && memSettingsRaw.String != "{}" {
				err = json.Unmarshal([]byte(memSettingsRaw.String), &m.Settings)
				if err != nil {
					numan_log.Warn(ctx).Err(err).Msg("Erreur lors de la désérialisation des paramètres du membre")
					return nil, numan_error.NewInternal()
				}
			}

			results = append(results, fullInboxResult{Conversation: c, Member: m})
		} else {
			numan_log.Error(ctx).Err(err).Msg("Erreur de scan des lignes Postgres")
		}
	}
	return results, nil
}
