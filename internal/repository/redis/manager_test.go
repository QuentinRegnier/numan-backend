package redis_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	goredis "github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserRecommendationRedisCollectionsAndPrimitives(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	// 1. Key formatting verification
	assert.Equal(t, "user:reco:list:geo:42", redis.UserRecoListGeo.Key(42))
	assert.Equal(t, "user:reco:list:aff:42", redis.UserRecoListAff.Key(42))
	assert.Equal(t, "user:reco:list:taste:42", redis.UserRecoListTaste.Key(42))
	assert.Equal(t, "user:reco:list:global:42", redis.UserRecoListGlobal.Key(42))
	assert.Equal(t, "users:lang:0", redis.UsersLang.Key(0))
	assert.Equal(t, "cuckoo:user_reco:skips:42", redis.CuckooUserRecoSkips.Key(42))
	assert.Equal(t, "lock:user_reco:compute:42", redis.LockUserRecoCompute.Key(42))

	// 2. SInter on UsersLang
	require.NoError(t, redis.UsersLang.SAdd(ctx, "0", 101, 102, 103))
	require.NoError(t, redis.UsersLang.SAdd(ctx, "1", 102, 103, 104))

	singleLang, err := redis.UsersLang.SInter(ctx, "0")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"101", "102", "103"}, singleLang)

	interLang, err := redis.UsersLang.SInter(ctx, "0", "1")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"102", "103"}, interLang)

	emptyLang, err := redis.UsersLang.SInter(ctx)
	require.NoError(t, err)
	assert.Empty(t, emptyLang)

	// 3. Cuckoo Filter operations
	require.NoError(t, redis.CuckooUserRecoSkips.CFReserve(ctx, 42, 3000))
	require.NoError(t, redis.CuckooUserRecoSkips.CFAdd(ctx, 42, 101))
	require.NoError(t, redis.CuckooUserRecoSkips.CFAddItems(ctx, 42, []any{102, 103}))

	exists101, err := redis.CuckooUserRecoSkips.CFExists(ctx, 42, 101)
	require.NoError(t, err)
	assert.True(t, exists101)

	exists999, err := redis.CuckooUserRecoSkips.CFExists(ctx, 42, 999)
	require.NoError(t, err)
	assert.False(t, exists999)

	batchExists, err := redis.CuckooUserRecoSkips.CFExistsMany(ctx, 42, []int64{101, 102, 103, 104})
	require.NoError(t, err)
	assert.True(t, batchExists[101])
	assert.True(t, batchExists[102])
	assert.True(t, batchExists[103])
	assert.False(t, batchExists[104])

	// 4. ZSetReplaceAtomic
	require.NoError(t, redis.UserRecoListGlobal.ZSetReplaceAtomic(ctx, 42, []*goredis.Z{
		{Score: 100.0, Member: 201},
		{Score: 90.0, Member: 202},
	}, time.Hour))

	members, err := redis.UserRecoListGlobal.ZRevRange(ctx, 42, 0, -1)
	require.NoError(t, err)
	assert.Equal(t, []string{"201", "202"}, members)

	// Replace with new members atomically
	require.NoError(t, redis.UserRecoListGlobal.ZSetReplaceAtomic(ctx, 42, []*goredis.Z{
		{Score: 200.0, Member: 301},
	}, time.Hour))
	membersAfter, err := redis.UserRecoListGlobal.ZRevRange(ctx, 42, 0, -1)
	require.NoError(t, err)
	assert.Equal(t, []string{"301"}, membersAfter)

	// 5. Compute Lock SetNX
	acquired, err := redis.LockUserRecoCompute.SetNX(ctx, 42, "1", 12*time.Hour)
	require.NoError(t, err)
	assert.True(t, acquired)

	acquiredAgain, err := redis.LockUserRecoCompute.SetNX(ctx, 42, "1", 12*time.Hour)
	require.NoError(t, err)
	assert.False(t, acquiredAgain)
}
