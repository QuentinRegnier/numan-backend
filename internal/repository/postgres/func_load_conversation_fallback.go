package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

type inboxFallbackResult struct {
	Conversation lite_models.ConvLiteRequest
	Member       lite_models.MemberLiteRequest
}

func FuncLoadConversationFallback(ctx context.Context, userID int64, convIDs []int64) ([]inboxFallbackResult, error) {
	query := `SELECT conversation_id, title, description, avatar_id, type, conversation_settings, last_message_id, role, settings, frozen_message_id, last_read_message_id, unread_count, joined_at FROM messaging.func_load_conversation_fallback($1, $2)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, userID, convIDs)
	if err != nil {
		return nil, numan_error.NewInternal(err)
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			numan_log.Error(ctx).Err(err).Msg("Erreur lors de la fermeture des lignes (Postgres)")
		}
	}(rows)

	var results []inboxFallbackResult
	for rows.Next() {
		var cid int64
		var title sql.NullString
		var description sql.NullString
		var avatarID sql.NullInt64
		var cType int
		var convSettingsRaw sql.NullString
		var lastMsgID sql.NullInt64
		var role, unreadCount int
		var frozenID sql.NullInt64
		var lastReadMsgID sql.NullInt64
		var joinedAt time.Time
		var memSettingsRaw sql.NullString
		var externalLink sql.NullString

		if err := rows.Scan(&cid, &title, &description, &avatarID, &cType, &convSettingsRaw, &lastMsgID, &role, &memSettingsRaw, &externalLink, &frozenID, &lastReadMsgID, &unreadCount, &joinedAt); err == nil {
			conv := lite_models.ConvLiteRequest{ID: cid, Type: cType}
			if title.Valid {
				conv.Title = title.String
			}
			if description.Valid {
				conv.Description = description.String
			}
			if avatarID.Valid {
				conv.AvatarID = avatarID.Int64
			}
			if lastMsgID.Valid {
				conv.LastMessageID = lastMsgID.Int64
			}
			if convSettingsRaw.Valid && convSettingsRaw.String != "" && convSettingsRaw.String != "{}" {
				err = json.Unmarshal([]byte(convSettingsRaw.String), &conv.Settings)
				if err != nil {
					numan_log.Warn(ctx).Err(err).Msg("Erreur lors de la désérialisation des paramètres de conversation")
					return nil, numan_error.NewInternal()
				}
			}
			if externalLink.Valid && externalLink.String != "" {
				err = json.Unmarshal([]byte(externalLink.String), &conv.ExternalLink)
				if err != nil {
					numan_log.Warn(ctx).Err(err).Msg("Erreur lors de la désérialisation des liens externes")
					return nil, numan_error.NewInternal()
				}
			}

			var parsedMemSettings member_models.MemberSettings
			if memSettingsRaw.Valid && memSettingsRaw.String != "" && memSettingsRaw.String != "{}" {
				err = json.Unmarshal([]byte(memSettingsRaw.String), &parsedMemSettings)
				if err != nil {
					numan_log.Warn(ctx).Err(err).Msg("Erreur lors de la désérialisation des paramètres du membre")
					return nil, numan_error.NewInternal()
				}
			}

			mem := lite_models.MemberLiteRequest{
				ConversationID: cid,
				UserID:         userID,
				Role:           role,
				Settings: lite_models.MemberSettingsLite{
					IsMuted:           parsedMemSettings.IsMuted,
					MuteExpireAt:      parsedMemSettings.MuteExpireAt,
					Pinned:            parsedMemSettings.Pinned,
					MediaAutoDownload: parsedMemSettings.MediaAutoDownload,
				},
				UnreadCount: unreadCount,
				JoinedAt:    joinedAt.UnixMilli(),
			}
			if frozenID.Valid {
				mem.FrozenMessageID = frozenID.Int64
			}
			if lastReadMsgID.Valid {
				mem.LastReadMessageID = lastReadMsgID.Int64
			}
			results = append(results, inboxFallbackResult{Conversation: conv, Member: mem})
		}
	}
	return results, nil
}
