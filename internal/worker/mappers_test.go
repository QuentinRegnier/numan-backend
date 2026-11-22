package worker

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConversationMapper_IncludesLangIsPartnerPinSlot(t *testing.T) {
	mapper := &conversationMapper{}
	cols := mapper.Columns()
	assert.Equal(t, []string{
		"id", "type", "title", "description", "avatar_id", "last_message_id",
		"state", "settings", "external_link", "lang", "is_partner", "pin_slot",
		"created_at", "updated_at",
	}, cols)

	pub := conversation_models.ConversationPayload{
		ID:        1001,
		Type:      variables.ConversationTypeCommunityPub,
		Title:     "Go France",
		State:     variables.ConversationStateActive,
		Lang:      "fr",
		IsPartner: true,
		PinSlot:   5,
		CreatedAt: 1700000000000,
		UpdatedAt: 1700000001000,
	}

	row, err := mapper.ToRow(pub)
	require.NoError(t, err)
	require.Len(t, row, len(cols))
	assert.Equal(t, "fr", row[9])
	assert.Equal(t, true, row[10])
	assert.Equal(t, 5, row[11])

	dm := conversation_models.ConversationPayload{
		ID:        2002,
		Type:      variables.ConversationTypeDirect,
		State:     variables.ConversationStateActive,
		CreatedAt: 1700000000000,
		UpdatedAt: 1700000001000,
	}
	dmRow, err := mapper.ToRow(dm)
	require.NoError(t, err)
	require.Len(t, dmRow, len(cols))
	assert.Nil(t, dmRow[9], "non-public conversation lang must be SQL NULL")
	assert.Equal(t, false, dmRow[10])
	assert.Nil(t, dmRow[11], "non-public conversation pin_slot must be SQL NULL")
}
