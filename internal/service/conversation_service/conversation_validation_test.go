package conversation_service_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/conversation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertConvServiceError(t *testing.T, err error, status int, code string) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, status, appErr.HTTPStatus)
	assert.Equal(t, code, appErr.Code)
}

func TestConversationServices_InvalidPayloads_NoRedisSideEffects(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	// Seed a private community group so we can also test JoinGroup 403 precedence vs validation
	seedConv := conversation_models.ConversationPayload{
		ID:    8001,
		Type:  variables.ConversationTypeGroup,
		Title: "Private Group",
		State: variables.ConversationStateActive,
	}
	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, seedConv))

	initialKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)

	// 1. CreateConversation invalid inputs & 403 preservation for communities
	_, err = conversation_service.CreateConversation(ctx, 0, conversation_models.CreateConversationInput{
		Type:           variables.ConversationTypeDirect,
		ParticipantIDs: []int64{2},
	})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = conversation_service.CreateConversation(ctx, 1, conversation_models.CreateConversationInput{
		Type:           variables.ConversationTypeGroup,
		Title:          strings.Repeat("a", 101),
		ParticipantIDs: []int64{2},
	})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = conversation_service.CreateConversation(ctx, 1, conversation_models.CreateConversationInput{
		Type:           variables.ConversationTypeCommunityPriv,
		Title:          "Comm",
		ParticipantIDs: []int64{2},
	})
	assertConvServiceError(t, err, http.StatusForbidden, numan_error.CodeForbidden)

	// 2. CreateCommunity invalid inputs
	_, err = conversation_service.CreateCommunity(ctx, 1, conversation_models.CreateCommunityInput{
		Title: "ab",
	})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = conversation_service.CreateCommunity(ctx, 1, conversation_models.CreateCommunityInput{
		Title:        "Valid Comm",
		ExternalLink: models.ExternalLinks{URL: "javascript:alert(1)"},
	})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	// 3. UpdateConversation invalid inputs
	_, err = conversation_service.UpdateConversation(ctx, 1, 0, conversation_models.UpdateConversationInput{
		ConversationID: 0,
		Title:          "New",
	})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = conversation_service.UpdateConversation(ctx, 1, 8001, conversation_models.UpdateConversationInput{
		ConversationID: 8001,
		Title:          strings.Repeat("é", 101),
	})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	// 4. AddMembersToConversation invalid inputs
	_, err = conversation_service.AddMembersToConversation(ctx, 1, conversation_models.AddMemberInput{
		ConversationID: 8001,
		ParticipantIDs: nil,
	})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = conversation_service.AddMembersToConversation(ctx, 1, conversation_models.AddMemberInput{
		ConversationID: 8001,
		ParticipantIDs: []int64{2, -1},
	})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	// 5. JoinGroup invalid inputs + 403 invite-required preservation
	_, err = conversation_service.JoinGroup(ctx, 1, conversation_models.JoinGroupInput{
		ConversationID: 8001,
		Internal:       true,
		External:       true,
	})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = conversation_service.JoinGroup(ctx, 1, conversation_models.JoinGroupInput{
		ConversationID: 8001,
	})
	assertConvServiceError(t, err, http.StatusForbidden, numan_error.CodeForbidden)

	// 6. LeaveConversation / Pin / Unpin / MarkConversationAsRead invalid inputs
	_, err = conversation_service.LeaveConversation(ctx, 1, conversation_models.LeaveConversationInput{ConversationID: 0})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = conversation_service.TogglePinConversation(ctx, 1, conversation_models.PinConversationInput{ConversationID: 0})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = conversation_service.UnpinConversation(ctx, 1, conversation_models.UnpinConversationInput{ConversationID: -1})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = conversation_service.MarkConversationAsRead(ctx, 1, 0)
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	// 7. Queries: GetConversations, GetUserConversationsPaginated, GetConversationMembers, GetWatermarks, SuggestContacts
	_, err = conversation_service.GetConversations(ctx, 1, conversation_models.GetConversationsInput{ConversationIDs: nil})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = conversation_service.GetUserConversationsPaginated(ctx, 1, conversation_models.GetUserConversationsInput{Limit: 101})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = conversation_service.GetConversationMembers(ctx, 1, member_models.GetConversationMembersInput{ConversationID: 0})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = conversation_service.GetWatermarks(ctx, 1, conversation_models.GetWatermarksInput{ConversationID: 0})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	_, err = conversation_service.SuggestContacts(ctx, 1, conversation_models.SuggestInput{Intent: "bad_intent"})
	assertConvServiceError(t, err, http.StatusBadRequest, numan_error.CodeInvalidPayload)

	// Verify no Redis side effects or queued DB writes occurred
	finalKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)
	assert.ElementsMatch(t, initialKeys, finalKeys)

	queueKeys, err := infraRedis.Rdb.Keys(ctx, "request_cache:q:*").Result()
	require.NoError(t, err)
	assert.Empty(t, queueKeys)
}
