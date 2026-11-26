package redis_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisCluster_CrossSlotEliminationAndShardedPubSub(t *testing.T) {
	srv := test.SetupTestRedisCluster(t)
	ctx := context.Background()

	require.True(t, infraRedis.IsClusterClient(infraRedis.CoreClient()), "CoreClient must be a *redis.ClusterClient")
	require.Equal(t, infraRedis.TopologyModeCluster, infraRedis.TierTopologyMode(infraRedis.CoreClient()))

	t.Run("MGet_GetMany_DeleteMany_Del_AcrossDifferentSlots", func(t *testing.T) {
		require.NoError(t, repoRedis.Users.SetObject(ctx, int64(101), map[string]any{"name": "alice"}))
		require.NoError(t, repoRedis.Users.SetObject(ctx, int64(202), map[string]any{"name": "bob"}))
		require.NoError(t, repoRedis.Users.SetObject(ctx, int64(303), map[string]any{"name": "charlie"}))

		rawVals, err := repoRedis.Users.MGet(ctx, int64(101), int64(202), int64(999), int64(303))
		require.NoError(t, err)
		require.Len(t, rawVals, 4)
		assert.NotNil(t, rawVals[0])
		assert.NotNil(t, rawVals[1])
		assert.Nil(t, rawVals[2])
		assert.NotNil(t, rawVals[3])

		res, err := repoRedis.Users.GetMany(ctx, []int64{101, 202, 303, 999})
		require.NoError(t, err)
		assert.Len(t, res.Found, 3)
		assert.Equal(t, []int64{999}, res.MissingIDs)

		require.NoError(t, repoRedis.Users.DeleteMany(ctx, []int64{101, 202}))
		exists101, err := repoRedis.Users.Exists(ctx, int64(101))
		require.NoError(t, err)
		assert.False(t, exists101)

		require.NoError(t, repoRedis.Users.DeleteObject(ctx, int64(303)))
		exists303, err := repoRedis.Users.Exists(ctx, int64(303))
		require.NoError(t, err)
		assert.False(t, exists303)

		// Also test ZUnionStore & Del across multiple ZSET keys in different slots
		require.NoError(t, repoRedis.RankedPosts.ZAdd(ctx, "slot_a", 10.0, "p1"))
		require.NoError(t, repoRedis.RankedPosts.ZAdd(ctx, "slot_b", 20.0, "p2"))
		require.NoError(t, repoRedis.RankedPosts.ZUnionStore(ctx, "slot_dest", []any{"slot_a", "slot_b"}, "MAX"))
		require.NoError(t, repoRedis.DeleteKeysOnCore(ctx, repoRedis.RankedPosts.Key("slot_a"), repoRedis.RankedPosts.Key("slot_b"), repoRedis.RankedPosts.Key("slot_dest")))
	})

	t.Run("SInter_and_PFCount_AcrossDifferentSlots", func(t *testing.T) {
		require.NoError(t, repoRedis.PostLikesSet.SAdd(ctx, int64(11), "u1", "u2", "u3"))
		require.NoError(t, repoRedis.PostLikesSet.SAdd(ctx, int64(22), "u2", "u3", "u4"))

		common, err := repoRedis.PostLikesSet.SInter(ctx, int64(11), int64(22))
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"u2", "u3"}, common)

		_, err = repoRedis.CommunityRecoAuthors.PFAdd(ctx, int64(1001), "a", "b")
		require.NoError(t, err)
		_, err = repoRedis.CommunityRecoAuthors.PFAdd(ctx, int64(2002), "b", "c")
		require.NoError(t, err)
		card, err := repoRedis.CommunityRecoAuthors.PFCount(ctx, int64(1001), int64(2002))
		require.NoError(t, err)
		assert.GreaterOrEqual(t, card, int64(2))
	})

	t.Run("WriteBehind_EnqueueDB_And_PopSmartBatchBlockingMulti_MultiShard", func(t *testing.T) {
		for _, id := range []int64{1, 2, 3, 4, 17, 33, 65} {
			require.NoError(t, repoRedis.EnqueueDB(ctx, id, id, repoRedis.EntityUser, repoRedis.ActionUpdate, map[string]any{"id": id}, repoRedis.TargetAll))
		}

		depth, err := repoRedis.TotalQueueDepth(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, depth, int64(7))

		ageSec, err := repoRedis.OldestQueueEventAgeSeconds(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, ageSec, 0.0)

		allShards := make([]int, repoRedis.QueueShards)
		for i := range allShards {
			allShards[i] = i
		}
		popped, err := repoRedis.PopSmartBatchBlockingMulti(ctx, allShards, 32)
		require.NoError(t, err)
		assert.NotEmpty(t, popped)
	})

	t.Run("DynamicVariables_HashTag_ClusterSafe", func(t *testing.T) {
		assert.Contains(t, repoRedis.DynamicVariables.Key("version"), "{dynamic_vars}")
		assert.Contains(t, repoRedis.DynamicVariables.Key("values"), "{dynamic_vars}")
		require.NoError(t, repoRedis.DynamicVariables.SetPrimitive(ctx, "version", "42"))
		val, err := repoRedis.DynamicVariables.GetBytes(ctx, "version")
		require.NoError(t, err)
		assert.Equal(t, "42", string(val))
	})

	t.Run("ClusterRegistry_ListClusterNodePayloads_ClusterSafe", func(t *testing.T) {
		now := time.Now().UnixMilli()
		require.NoError(t, repoRedis.RegisterClusterNode(ctx, "fr-par", "api-0", map[string]any{
			"role":       "api_node",
			"node_id":    1,
			"updated_at": now,
		}, 30*time.Second))
		require.NoError(t, repoRedis.RegisterClusterNode(ctx, "fr-par", "worker-0", map[string]any{
			"role":       "worker_node",
			"node_id":    2,
			"updated_at": now,
		}, 30*time.Second))

		payloads, err := repoRedis.ListClusterNodePayloads(ctx, "fr-par")
		require.NoError(t, err)
		assert.Len(t, payloads, 2)
	})

	t.Run("ObjectCacheArticleService_MultiKeyDel_ClusterSafe", func(t *testing.T) {
		art := article_models.ArticlePayload{
			ID:         777,
			Title:      "Cluster Safe Article",
			Visibility: 1,
			CreatedAt:  time.Now().UnixMilli(),
		}
		require.NoError(t, object_cache_service.SetArticleInCache(ctx, art))
		got, err := object_cache_service.GetArticleFromCache(ctx, 777)
		require.NoError(t, err)
		assert.Equal(t, int64(777), got.ID)

		require.NoError(t, object_cache_service.DeleteArticleFromCache(ctx, 777))
		require.NoError(t, repoRedis.DeleteKeysOnObjects(ctx, "articles:index:data", "articles:index:order", "articles:index:init"))
	})

	t.Run("ChannelUser_PublishMultiple_UsesShardedPubSub_SPublish", func(t *testing.T) {
		before := srv.SPublishCalls.Load()
		require.NoError(t, repoRedis.ChannelUser.PublishMultiple(ctx, []int64{10, 20, 30}, []byte(`{"type":"ping"}`)))
		assert.GreaterOrEqual(t, srv.SPublishCalls.Load()-before, int64(3), "Cluster mode must route Pub/Sub via SPUBLISH")
	})
}

func TestRedisMultiTier_StrictCollectionIsolation(t *testing.T) {
	_, _, _ = test.SetupTestRedisMultiTier(t)
	ctx := context.Background()

	require.True(t, infraRedis.HasDedicatedObjectPool())
	require.True(t, infraRedis.HasDedicatedPubSub())

	// Tier 1 (Core): Sessions
	require.NoError(t, repoRedis.Sessions.SetPrimitive(ctx, "sess-1", "tok"))
	// Tier 2 (Objects): Posts
	require.NoError(t, repoRedis.Posts.SetPrimitive(ctx, int64(99), "post-data"))
	// Tier 3 (PubSub): Presence
	require.NoError(t, repoRedis.Presence.SetPrimitive(ctx, int64(42), "online"))

	// Verify strict isolation via direct tier clients
	coreVal, err := infraRedis.CoreClient().Get(ctx, repoRedis.Sessions.Key("sess-1")).Result()
	require.NoError(t, err)
	assert.Equal(t, "tok", coreVal)
	assert.Error(t, infraRedis.ObjectClient().Get(ctx, repoRedis.Sessions.Key("sess-1")).Err())

	objVal, err := infraRedis.ObjectClient().Get(ctx, repoRedis.Posts.Key(int64(99))).Result()
	require.NoError(t, err)
	assert.Equal(t, "post-data", objVal)
	assert.Error(t, infraRedis.CoreClient().Get(ctx, repoRedis.Posts.Key(int64(99))).Err())

	pubVal, err := infraRedis.PubSubClient().Get(ctx, repoRedis.Presence.Key(int64(42))).Result()
	require.NoError(t, err)
	assert.Equal(t, "online", pubVal)
	assert.Error(t, infraRedis.CoreClient().Get(ctx, repoRedis.Presence.Key(int64(42))).Err())
}
