package relation_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/relation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/relation_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestRelationServices_InvalidPayloads_NoRedisSideEffects(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	initialKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)

	// ToggleFollow
	assertBadRequest(t, relation_service.ToggleFollow(ctx, 0, relation_models.RelationActionInput{TargetID: 2, Action: true}))
	assertBadRequest(t, relation_service.ToggleFollow(ctx, 1, relation_models.RelationActionInput{TargetID: 0, Action: true}))
	assertBadRequest(t, relation_service.ToggleFollow(ctx, 1, relation_models.RelationActionInput{TargetID: 1, Action: true}))

	// ToggleFriend
	assertBadRequest(t, relation_service.ToggleFriend(ctx, 0, relation_models.RelationActionInput{TargetID: 2, Action: true}))
	assertBadRequest(t, relation_service.ToggleFriend(ctx, 1, relation_models.RelationActionInput{TargetID: -1, Action: false}))
	assertBadRequest(t, relation_service.ToggleFriend(ctx, 1, relation_models.RelationActionInput{TargetID: 1, Action: true}))

	// ToggleBlock
	assertBadRequest(t, relation_service.ToggleBlock(ctx, 0, relation_models.RelationActionInput{TargetID: 2, Action: true}))
	assertBadRequest(t, relation_service.ToggleBlock(ctx, 1, relation_models.RelationActionInput{TargetID: 0, Action: false}))
	assertBadRequest(t, relation_service.ToggleBlock(ctx, 1, relation_models.RelationActionInput{TargetID: 1, Action: true}))

	// GetFollows
	_, err = relation_service.GetFollows(ctx, 0, relation_models.GetFollowersInput{TargetID: 2})
	assertBadRequest(t, err)
	_, err = relation_service.GetFollows(ctx, 1, relation_models.GetFollowersInput{TargetID: 0})
	assertBadRequest(t, err)
	_, err = relation_service.GetFollows(ctx, 1, relation_models.GetFollowersInput{TargetID: 2, Limit: 101})
	assertBadRequest(t, err)
	_, err = relation_service.GetFollows(ctx, 1, relation_models.GetFollowersInput{TargetID: 2, Offset: -1})
	assertBadRequest(t, err)

	// GetFriends
	_, err = relation_service.GetFriends(ctx, 0, relation_models.GetFriendsInput{TargetID: 2})
	assertBadRequest(t, err)
	_, err = relation_service.GetFriends(ctx, 1, relation_models.GetFriendsInput{TargetID: 0})
	assertBadRequest(t, err)
	_, err = relation_service.GetFriends(ctx, 1, relation_models.GetFriendsInput{TargetID: 2, Limit: -1})
	assertBadRequest(t, err)

	// GetBlockedUsers
	_, err = relation_service.GetBlockedUsers(ctx, 0, relation_models.GetBlockedInput{})
	assertBadRequest(t, err)
	_, err = relation_service.GetBlockedUsers(ctx, 1, relation_models.GetBlockedInput{Limit: 101})
	assertBadRequest(t, err)
	_, err = relation_service.GetBlockedUsers(ctx, 1, relation_models.GetBlockedInput{Offset: -1})
	assertBadRequest(t, err)

	finalKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)
	assert.ElementsMatch(t, initialKeys, finalKeys)

	queueKeys, err := infraRedis.Rdb.Keys(ctx, "request_cache:q:*").Result()
	require.NoError(t, err)
	assert.Empty(t, queueKeys)
}
