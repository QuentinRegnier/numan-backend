package service_test

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestToConvLiteRequest_PreservesAllFieldsAndMsgPackRoundTrip(t *testing.T) {
	src := conversation_models.ConversationPayload{
		ID:            9001,
		Type:          variables.ConversationTypeCommunityPub,
		Title:         "Go Builders",
		Description:   "Public Go community",
		AvatarID:      777,
		LastMessageID: 888,
		State:         variables.ConversationStateActive,
		Settings: conversation_models.ConversationSettings{
			WritePermission:      0, // free_chat
			JoinApprovalRequired: true,
			SendMediaPermission:  true,
			AddMemberPermission:  false,
			HideSystemMessages:   true,
			JoinWithLinkDuration: 123456,
			SendSurveyPermission: true,
		},
		ExternalLink: models.ExternalLinks{Title: "Site", URL: "https://example.com"},
		Lang:         "fr",
		IsPartner:    true,
		PinSlot:      4,
		CreatedAt:    1700000000000,
		UpdatedAt:    1700000001000,
	}

	lite := service.ToConvLiteRequest(src)
	require.NoError(t, lite.Validate())
	assert.Equal(t, src.ID, lite.ID)
	assert.Equal(t, src.Type, lite.Type)
	assert.Equal(t, src.Title, lite.Title)
	assert.Equal(t, src.Description, lite.Description)
	assert.Equal(t, src.AvatarID, lite.AvatarID)
	assert.Equal(t, src.LastMessageID, lite.LastMessageID)
	assert.Equal(t, src.ExternalLink, lite.ExternalLink)
	assert.Equal(t, "fr", lite.Lang)
	assert.True(t, lite.IsPartner)
	assert.Equal(t, 4, lite.PinSlot)
	assert.Equal(t, string(models.CommunityModeFreeChat), lite.Mode)

	// Verify MsgPack round-trip preserves all fields
	encoded, err := msgpack.Marshal(lite)
	require.NoError(t, err)

	var decoded lite_models.ConvLiteRequest
	require.NoError(t, msgpack.Unmarshal(encoded, &decoded))
	assert.Equal(t, lite, decoded)
	assert.Equal(t, models.CommunityModeFreeChat, lite_models.ResolveCommunityMode(decoded))
}

func TestToConvLiteRequest_NonPublicConversationHasEmptyCommunityFields(t *testing.T) {
	dm := conversation_models.ConversationPayload{
		ID:    42,
		Type:  variables.ConversationTypeDirect,
		State: variables.ConversationStateActive,
	}
	lite := service.ToConvLiteRequest(dm)
	assert.Empty(t, lite.Lang)
	assert.False(t, lite.IsPartner)
	assert.Zero(t, lite.PinSlot)
	assert.Empty(t, lite.Mode)
}
