package redis_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTotalQueueDepth_SumsAcrossShards(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	depth, err := repoRedis.TotalQueueDepth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), depth)

	require.NoError(t, repoRedis.EnqueueDB(ctx, 1001, 0, repoRedis.EntityPost, repoRedis.ActionCreate, map[string]any{"id": 1001}, repoRedis.TargetAll))
	require.NoError(t, repoRedis.EnqueueDB(ctx, 2002, 0, repoRedis.EntityLike, repoRedis.ActionCreate, map[string]any{"id": 2002}, repoRedis.TargetPostgres))
	require.NoError(t, repoRedis.EnqueueDB(ctx, 3003, 0, repoRedis.EntityMessage, repoRedis.ActionCreate, map[string]any{"id": 3003}, repoRedis.TargetMongo))

	depth, err = repoRedis.TotalQueueDepth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), depth)
}
