package member_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/member_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertMemberServiceBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestMemberServices_InvalidPayloads_NoRedisSideEffects(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	initialKeys, err := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, err)

	_, err = member_service.AcceptCommunityRequest(ctx, 1, member_models.AcceptCommunityRequestInput{ConversationID: 0, TargetUserID: 2})
	assertMemberServiceBadRequest(t, err)

	_, err = member_service.RefuseCommunityRequest(ctx, 1, member_models.RefuseCommunityRequestInput{ConversationID: 1, TargetUserID: 0})
	assertMemberServiceBadRequest(t, err)

	_, err = member_service.BanMember(ctx, 1, member_models.BanMemberInput{ConversationID: 0, TargetUserID: 2})
	assertMemberServiceBadRequest(t, err)

	_, err = member_service.PromoteMember(ctx, 1, member_models.PromoteMemberInput{ConversationID: 1, TargetUserID: 0})
	assertMemberServiceBadRequest(t, err)

	_, err = member_service.DemoteMember(ctx, 1, member_models.DemoteMemberInput{ConversationID: 0, TargetUserID: 2})
	assertMemberServiceBadRequest(t, err)

	err = member_service.MuteMember(ctx, 1, member_models.MuteMemberInput{ConversationID: 1, TargetUserID: 2, RestrictedUntil: -5})
	assertMemberServiceBadRequest(t, err)

	_, err = member_service.UnbanMembers(ctx, 1, member_models.UnbanMembersInput{ConversationID: 1, TargetUserIDs: nil})
	assertMemberServiceBadRequest(t, err)

	_, err = member_service.UpdateMemberSettings(ctx, 1, member_models.UpdateMemberSettingsInput{ConversationID: 1, IsMuted: 9})
	assertMemberServiceBadRequest(t, err)

	_, err = member_service.GetBannedMembers(ctx, 1, member_models.GetBannedMembersInput{ConversationID: 0})
	assertMemberServiceBadRequest(t, err)

	_, err = member_service.GetCommunityRequests(ctx, 1, member_models.GetCommunityRequestsInput{ConversationID: 1, Limit: 101})
	assertMemberServiceBadRequest(t, err)

	_, err = member_service.GetMutedMembers(ctx, 1, member_models.GetMutedMembersInput{ConversationID: 1, Offset: -1})
	assertMemberServiceBadRequest(t, err)

	// Invalid callerID also rejected cleanly
	_, err = member_service.UpdateMemberSettings(ctx, 0, member_models.UpdateMemberSettingsInput{ConversationID: 1})
	assertMemberServiceBadRequest(t, err)

	finalKeys, err := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, err)
	assert.ElementsMatch(t, initialKeys, finalKeys)

	queueKeys, err := repoRedis.ScanKeys(ctx, "request_cache:q:*", 200)
	require.NoError(t, err)
	assert.Empty(t, queueKeys)
}
