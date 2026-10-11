package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

func FuncGetConversation(ctx context.Context, convID int64) (conversation_models.ConversationPayload, error) {
	query := `SELECT id, type, title, description, avatar_id, last_message_id, state, settings, created_at, updated_at FROM messaging.func_get_conversation($1)`
	var c conversation_models.ConversationPayload
	var cTitle sql.NullString
	var cDescription sql.NullString
	var cAvatarID sql.NullInt64
	var cLastMsgID sql.NullInt64
	var settingsRaw sql.NullString
	var externalLinkRaw sql.NullString

	err := postgres.PostgresDB.QueryRowContext(ctx, query, convID).Scan(
		&c.ID, &c.Type, &cTitle, &cDescription, &cAvatarID, &cLastMsgID, &c.State, &settingsRaw, &externalLinkRaw, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c, numan_error.NewNotFound("CONV_NOT_FOUND", "Conversation introuvable.", err)
		}
		numan_log.Error(ctx).Err(err).Msg("Erreur SQL inattendue lors de la vérification de l'enregistrement")
		return c, numan_error.NewInternal()
	}

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
	if settingsRaw.Valid && settingsRaw.String != "" && settingsRaw.String != "{}" {
		err = json.Unmarshal([]byte(settingsRaw.String), &c.Settings)
		if err != nil {
			numan_log.Warn(ctx).Err(err).Msg("Erreur lors de la désérialisation des paramètres de conversation")
			return c, numan_error.NewInternal()
		}
	}
	if externalLinkRaw.Valid && externalLinkRaw.String != "" && externalLinkRaw.String != "{}" {
		err = json.Unmarshal([]byte(externalLinkRaw.String), &c.ExternalLink)
		if err != nil {
			numan_log.Warn(ctx).Err(err).Msg("Erreur lors de la désérialisation des liens externes")
			return c, numan_error.NewInternal()
		}
	}
	return c, nil
}
