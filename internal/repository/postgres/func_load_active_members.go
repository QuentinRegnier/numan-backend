package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

type activeMemberResult struct {
	Member        lite_models.MemberLiteRequest
	LastMessageID sql.NullInt64
}

// FuncLoadActiveMembers récupère les membres pour le Seeding de l'Inbox
func FuncLoadActiveMembers(ctx context.Context) ([]activeMemberResult, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT conversation_id, user_id, role, settings, unread_count, frozen_message_id, last_read_message_id, last_message_id, joined_at FROM messaging.func_load_active_members()`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les membres actifs.",
			err,
		)
	}
	defer func() { _ = rows.Close() }()

	results := make([]activeMemberResult, 0)
	for rows.Next() {
		var cid, uid int64
		var role, unreadCount int
		var settingsRaw sql.NullString
		var lastMsgID sql.NullInt64
		var lastReadMessageID sql.NullInt64
		var frozenID sql.NullInt64
		var joinedAt time.Time

		if err := rows.Scan(&cid, &uid, &role, &settingsRaw, &unreadCount, &frozenID, &lastReadMessageID, &lastMsgID, &joinedAt); err == nil {

			// PARSING DU JSONB VERS LA STRUCT GO
			var parsedSettings member_models.MemberSettings
			if settingsRaw.Valid && settingsRaw.String != "" && settingsRaw.String != "{}" {
				if err := json.Unmarshal([]byte(settingsRaw.String), &parsedSettings); err != nil {
					return nil, numan_error.NewAppError(
						http.StatusInternalServerError,
						numan_error.CodeDatabaseError,
						"Impossible de désérialiser les paramètres du membre.",
						err,
					)
				}
			}

			mem := lite_models.MemberLiteRequest{
				ConversationID: cid,
				UserID:         uid,
				Role:           role,
				Settings: lite_models.MemberSettingsLite{
					IsMuted:           parsedSettings.IsMuted,
					MuteExpireAt:      parsedSettings.MuteExpireAt,
					Pinned:            parsedSettings.Pinned,
					MediaAutoDownload: parsedSettings.MediaAutoDownload,
				},
				UnreadCount: unreadCount,
				JoinedAt:    joinedAt.UnixMilli(),
			}

			if frozenID.Valid {
				mem.FrozenMessageID = frozenID.Int64
			}
			if lastReadMessageID.Valid {
				mem.LastReadMessageID = lastReadMessageID.Int64
			}

			results = append(results, activeMemberResult{Member: mem, LastMessageID: lastMsgID})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les membres actifs.",
			err,
		)
	}

	return results, nil
}
