package redis_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnqueueDB_ActionGenerateUserRecommendations(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	payload := user_recommendation_models.GenerateUserRecommendationsJobPayload{
		UserID:      12345,
		Force:       true,
		TriggeredBy: "test",
		RequestedAt: 1700000000000,
	}

	err := redis.EnqueueDB(
		ctx,
		12345,
		12345,
		redis.EntityUserRecommendation,
		redis.ActionGenerateUserRecommendations,
		payload,
		redis.TargetWorker,
	)
	require.NoError(t, err)

	shardIdx := redis.GetShardID(12345)
	batch, errPop := redis.PopSmartBatchBlocking(ctx, shardIdx, 10)
	require.NoError(t, errPop)
	require.Len(t, batch, 1)
	assert.Equal(t, int64(12345), batch[0].ID)
	assert.Equal(t, redis.EntityUserRecommendation, batch[0].Type)
	assert.Equal(t, redis.ActionGenerateUserRecommendations, batch[0].Action)
	assert.Equal(t, redis.TargetWorker, batch[0].Targets)
}
