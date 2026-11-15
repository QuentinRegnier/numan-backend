package message_service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertMsgServiceError(t *testing.T, err error, status int, code string) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, status, appErr.HTTPStatus)
	assert.Equal(t, code, appErr.Code)
}

func TestMessageServices_InvalidPayloads_NoRedisSideEffects(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	// Seed conversation and member to ensure internal system messages still work while invalid payloads are blocked early
	convID := int64(9100)
	senderID := int64(50)
	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, conversation_models.ConversationPayload{
		ID:    convID,
		Type:  variables.ConversationTypeGroup,
		Title: "Valid Group",
		State: variables.ConversationStateActive,
	}))
	require.NoError(t, object_cache_service.SetMemberInObjectCache(ctx, member_models.MemberPayload{
		ID:             1,
		ConversationID: convID,
		UserID:         senderID,
		Role:           variables.MemberRoleNormal,
	}))

	initialKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)

	// 1. External system message must still return 403 Forbidden (anti-usurpation precedence)
	_, err = CreateMessage(ctx, senderID, convID, message_models.CreateMessageInput{
		ConversationID: convID,
		MessageType:    variables.MessageTypeSystem,
		Content:        "system",
	}, false)
	assertMsgServiceError(t, err, http.StatusForbidden, numan_error.CodeForbidden)

	// 2. Invalid external CreateMessage inputs return 400 Bad Request
	_, err = CreateMessage(ctx, senderID, convID, message_models.CreateMessageInput{
		ConversationID: convID,
		MessageType:    variables.MessageTypeText,
		Content:        "   ",
	}, false)
	assertMsgServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = CreateMessage(ctx, senderID, convID, message_models.CreateMessageInput{
		ConversationID: convID,
		MessageType:    variables.MessageTypeText,
		Content:        strings.Repeat("é", 2201),
	}, false)
	assertMsgServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = CreateMessage(ctx, senderID, convID, message_models.CreateMessageInput{
		ConversationID: convID,
		MessageType:    variables.MessageTypeMedia,
		Attachments:    nil,
	}, false)
	assertMsgServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	// 3. UpdateMessage & DeleteMessage invalid inputs
	_, err = UpdateMessage(ctx, senderID, message_models.UpdateMessageInput{MessageID: 0, Content: "hello"})
	assertMsgServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = UpdateMessage(ctx, senderID, message_models.UpdateMessageInput{MessageID: 1, Content: "   "})
	assertMsgServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = DeleteMessage(ctx, senderID, message_models.DeleteMessageInput{MessageID: 0})
	assertMsgServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	// 4. Queries & Reactions invalid inputs
	_, err = GetMessages(ctx, senderID, message_models.GetMessagesInput{ConversationID: convID, Direction: "bad"})
	assertMsgServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = GetMessageReactions(ctx, senderID, message_models.GetMessageReactionsInput{MessageID: 0})
	assertMsgServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	err = ReactToMessage(ctx, senderID, message_models.ReactMessageInput{MessageID: 1, Reaction: "   "})
	assertMsgServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	err = UnreactToMessage(ctx, senderID, message_models.UnreactMessageInput{MessageID: 0})
	assertMsgServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	finalKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)
	assert.ElementsMatch(t, initialKeys, finalKeys)

	queueKeys, err := infraRedis.Rdb.Keys(ctx, "request_cache:q:*").Result()
	require.NoError(t, err)
	assert.Empty(t, queueKeys)

	// 5. Internal call compatibility: callers omit input.ConversationID and pass it in param 3
	out, err := CreateMessage(ctx, senderID, convID, message_models.CreateMessageInput{
		MessageType: variables.MessageTypeSystem,
		Content:     "User joined",
	}, true)
	require.NoError(t, err)
	assert.NotZero(t, out.MessageID)
}
