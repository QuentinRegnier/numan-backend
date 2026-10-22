package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncGetMember récupère l'intégralité d'un membre depuis L3
func FuncGetMember(ctx context.Context, convID int64, userID int64) (member_models.MemberPayload, error) {
	if postgres.PostgresDB == nil {
		return member_models.MemberPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}

	query := `SELECT id, conversation_id, user_id, role, settings, joined_at, unread_count, frozen_message_id, last_read_message_id, created_at, updated_at FROM messaging.func_get_member($1, $2)`

	var m member_models.MemberPayload
	var frozenID sql.NullInt64
	var lastID sql.NullInt64
	var settingsRaw sql.NullString

	err := postgres.PostgresDB.QueryRowContext(ctx, query, convID, userID).Scan(
		&m.ID, &m.ConversationID, &m.UserID, &m.Role, &settingsRaw, &m.JoinedAt, &m.UnreadCount, &frozenID, &lastID, &m.CreatedAt, &m.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return member_models.MemberPayload{}, numan_error.NewNotFound(
				"MEMBER_NOT_FOUND",
				"Membre introuvable.",
				fmt.Errorf("member (%d, %d) was not found in PostgreSQL (L3)", convID, userID),
			)
		}
		return member_models.MemberPayload{}, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de récupérer les informations du membre.",
			err,
		)
	}

	if frozenID.Valid {
		m.FrozenMessageID = frozenID.Int64
	}
	if lastID.Valid {
		m.LastReadMessageID = lastID.Int64
	}

	// PARSING DU JSONB
	if settingsRaw.Valid && settingsRaw.String != "" && settingsRaw.String != "{}" {
		if err := json.Unmarshal([]byte(settingsRaw.String), &m.Settings); err != nil {
			return member_models.MemberPayload{}, numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeDatabaseError,
				"Impossible de désérialiser les paramètres du membre.",
				err,
			)
		}
	}

	return m, nil
}
