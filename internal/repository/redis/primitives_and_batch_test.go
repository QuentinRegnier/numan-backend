package redis_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type batchSample struct {
	ID   int64  `msgpack:"id"`
	Name string `msgpack:"name"`
}

func TestBatchOperations_StandaloneAndCluster(t *testing.T) {
	modes := []struct {
		name  string
		setup func(*testing.T) *test.FakeRedisServer
	}{
		{"Standalone", test.SetupTestRedis},
		{"Cluster", test.SetupTestRedisCluster},
	}

	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			srv := mode.setup(t)
			ctx := context.Background()

			// 1. Empty inputs return cleanly with zero errors
			vals, err := repoRedis.Users.MGet(ctx)
			require.NoError(t, err)
			assert.Nil(t, vals)

			gmEmpty, err := repoRedis.Users.GetMany(ctx, nil)
			require.NoError(t, err)
			assert.Empty(t, gmEmpty.Found)
			assert.Empty(t, gmEmpty.MissingIDs)

			require.NoError(t, repoRedis.Users.SetMany(ctx, nil))
			require.NoError(t, repoRedis.Users.DeleteMany(ctx, nil))
			require.NoError(t, repoRedis.Users.UnlinkMany(ctx, nil))
			require.NoError(t, repoRedis.Users.DeleteKeys(ctx))
			require.NoError(t, repoRedis.Users.UnlinkKeys(ctx))

			// 2. SetMany + GetMany (duplicates, missing keys, deterministic missing order)
			require.NoError(t, repoRedis.Users.SetMany(ctx, map[int64]any{
				101: batchSample{ID: 101, Name: "alice"},
				102: batchSample{ID: 102, Name: "bob"},
			}))

			gm, err := repoRedis.Users.GetMany(ctx, []int64{101, 999, 102, 101, 888, 999})
			require.NoError(t, err)
			assert.Len(t, gm.Found, 2)
			assert.Contains(t, gm.Found, int64(101))
			assert.Contains(t, gm.Found, int64(102))
			assert.Equal(t, []int64{999, 888}, gm.MissingIDs)

			// 3. MGet preserves 1:1 positional ordering including duplicates and missing keys
			mvals, err := repoRedis.Users.MGet(ctx, int64(101), int64(777), int64(102), int64(101))
			require.NoError(t, err)
			require.Len(t, mvals, 4)
			assert.NotNil(t, mvals[0])
			assert.Nil(t, mvals[1])
			assert.NotNil(t, mvals[2])
			assert.Equal(t, mvals[0], mvals[3])

			// 4. UnlinkMany & DeleteKeys remove keys cleanly (even with duplicates)
			require.NoError(t, repoRedis.Users.UnlinkMany(ctx, []int64{101, 101}))
			exists101, err := repoRedis.Users.Exists(ctx, int64(101))
			require.NoError(t, err)
			assert.False(t, exists101)

			require.NoError(t, repoRedis.Users.DeleteKeys(ctx, int64(102), int64(102)))
			exists102, err := repoRedis.Users.Exists(ctx, int64(102))
			require.NoError(t, err)
			assert.False(t, exists102)

			// 5. Context cancellation stops batch immediately
			canceledCtx, cancel := context.WithCancel(ctx)
			cancel()
			_, err = repoRedis.Users.GetMany(canceledCtx, []int64{101, 102})
			assert.ErrorIs(t, err, context.Canceled)
			err = repoRedis.Users.SetMany(canceledCtx, map[int64]any{101: batchSample{ID: 101}})
			assert.ErrorIs(t, err, context.Canceled)
			err = repoRedis.Users.DeleteMany(canceledCtx, []int64{101})
			assert.ErrorIs(t, err, context.Canceled)
			err = repoRedis.Users.UnlinkMany(canceledCtx, []int64{101})
			assert.ErrorIs(t, err, context.Canceled)

			// 6. Partial failure propagation (must never silently swallow command errors)
			failCmd := "MGET"
			if mode.name == "Cluster" {
				failCmd = "GET"
			}
			srv.SetFailCmd(failCmd, errors.New("injected batch failure"))
			_, err = repoRedis.Users.GetMany(ctx, []int64{101, 102})
			assert.Error(t, err)
			srv.SetFailCmd(failCmd, nil)
		})
	}
}

func TestHashSetZSetHLLBatch_StandaloneAndCluster(t *testing.T) {
	modes := []struct {
		name  string
		setup func(*testing.T) *test.FakeRedisServer
	}{
		{"Standalone", test.SetupTestRedis},
		{"Cluster", test.SetupTestRedisCluster},
	}

	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			_ = mode.setup(t)
			ctx := context.Background()

			// Hash batch + HGet / HGetBytes / HGetAll / HLen / HExists / HIncrByWithExpireNX
			require.NoError(t, repoRedis.GraphEdges.HSetMapBatch(ctx, map[any]map[string]any{
				"go":    {"redis": "10", "cluster": "20"},
				"rust":  {"wasm": "5"},
				"empty": {},
			}))

			v, err := repoRedis.GraphEdges.HGet(ctx, "go", "redis")
			require.NoError(t, err)
			assert.Equal(t, "10", v)

			vb, err := repoRedis.GraphEdges.HGetBytes(ctx, "go", "cluster")
			require.NoError(t, err)
			assert.Equal(t, []byte("20"), vb)

			all, err := repoRedis.GraphEdges.HGetAll(ctx, "go")
			require.NoError(t, err)
			assert.Equal(t, map[string]string{"redis": "10", "cluster": "20"}, all)

			hlen, err := repoRedis.GraphEdges.HLen(ctx, "go")
			require.NoError(t, err)
			assert.Equal(t, int64(2), hlen)

			hexists, err := repoRedis.GraphEdges.HExists(ctx, "go", "redis")
			require.NoError(t, err)
			assert.True(t, hexists)

			rows, err := repoRedis.GraphEdges.HMGetBatch(ctx, []any{"go", "rust", "missing"}, []string{"redis", "wasm"})
			require.NoError(t, err)
			require.Len(t, rows, 3)
			assert.Equal(t, "10", rows[0][0])
			assert.Equal(t, "5", rows[1][1])
			assert.Nil(t, rows[2][0])

			inc, err := repoRedis.CommunityRecoMsgCount.HIncrByWithExpireNX(ctx, "2026-11-26", "5001", 3, time.Hour)
			require.NoError(t, err)
			assert.Equal(t, int64(3), inc)

			// Set batch (SAddBatch, SMembersBatch, SCardBatch, SInter)
			require.NoError(t, repoRedis.SpeedRelations.SAddBatch(ctx, map[int64][]int64{
				1: {10, 20, 30},
				2: {20, 30, 40},
			}))

			membersMap, err := repoRedis.SpeedRelations.SMembersBatch(ctx, []int64{1, 2, 1, 999})
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"10", "20", "30"}, membersMap[1])
			assert.ElementsMatch(t, []string{"20", "30", "40"}, membersMap[2])
			assert.Empty(t, membersMap[999])

			cards, err := repoRedis.SpeedRelations.SCardBatch(ctx, []int64{1, 2, 999})
			require.NoError(t, err)
			assert.Equal(t, int64(3), cards[1])
			assert.Equal(t, int64(3), cards[2])
			assert.Equal(t, int64(0), cards[999])

			inter, err := repoRedis.SpeedRelations.SInter(ctx, int64(1), int64(2))
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"20", "30"}, inter)

			// ZSet batch (ZAddAndTrimBatch with trimStop=-3 keeps top 2 highest-scored items, ZRevRangeBatch)
			require.NoError(t, repoRedis.FeedsMailbox.ZAddAndTrimBatch(ctx, []int64{10, 20}, 100.0, "post_1", -3))
			require.NoError(t, repoRedis.FeedsMailbox.ZAddAndTrimBatch(ctx, []int64{10, 20}, 200.0, "post_2", -3))
			require.NoError(t, repoRedis.FeedsMailbox.ZAddAndTrimBatch(ctx, []int64{10}, 300.0, "post_3", -3))

			zrows, err := repoRedis.FeedsMailbox.ZRevRangeBatch(ctx, []any{int64(10), int64(20), int64(999)}, 0, 5)
			require.NoError(t, err)
			require.Len(t, zrows, 3)
			assert.Equal(t, []string{"post_3", "post_2"}, zrows[0]) // trimmed to top 2
			assert.Equal(t, []string{"post_2", "post_1"}, zrows[1])
			assert.Empty(t, zrows[2])

			// HLL batch & pipeline (CRITICAL: verifies zero CROSSSLOT in Cluster mode!)
			require.NoError(t, repoRedis.RecordCommunityMessageActivityPipeline(
				ctx,
				repoRedis.CommunityRecoMsgCount,
				repoRedis.CommunityRecoAuthors,
				"2026-11-26",
				"100:2026-11-26",
				"100",
				"u1",
				time.Hour,
			))
			require.NoError(t, repoRedis.RecordCommunityMessageActivityPipeline(
				ctx,
				repoRedis.CommunityRecoMsgCount,
				repoRedis.CommunityRecoAuthors,
				"2026-11-25",
				"100:2026-11-25",
				"100",
				"u2",
				time.Hour,
			))

			counts, err := repoRedis.CommunityRecoAuthors.PFCountBatch(ctx, [][]any{
				{"100:2026-11-26", "100:2026-11-25"},
				{"200:2026-11-26"},
			})
			require.NoError(t, err)
			require.Len(t, counts, 2)
			assert.Equal(t, int64(2), counts[0])
			assert.Equal(t, int64(0), counts[1])
		})
	}
}

func TestPrecompiledLuaScriptsAndLocks(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	// 1. ApplyVariablesPatchAtomic & LoadVariablesStateAtomic
	ver, err := repoRedis.DynamicVariables.ApplyVariablesPatchAtomic(
		ctx,
		"overrides",
		"version",
		map[string]string{"a": "1", "b": "2"},
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), ver)

	ver2, err := repoRedis.DynamicVariables.ApplyVariablesPatchAtomic(
		ctx,
		"overrides",
		"version",
		map[string]string{"b": "20", "c": "3"},
		[]string{"a"},
	)
	require.NoError(t, err)
	assert.Equal(t, int64(2), ver2)

	overrides, loadedVer, err := repoRedis.DynamicVariables.LoadVariablesStateAtomic(ctx, "overrides", "version")
	require.NoError(t, err)
	assert.Equal(t, int64(2), loadedVer)
	assert.Equal(t, map[string]string{"b": "20", "c": "3"}, overrides)

	// 2. Distributed CronLocks (token-protected release via precompiled Lua script)
	tok, ok, err := repoRedis.AcquireCronLock(ctx, "trend_worker", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, tok)

	// Second acquisition fails while locked
	_, ok2, err := repoRedis.AcquireCronLock(ctx, "trend_worker", time.Minute)
	require.NoError(t, err)
	assert.False(t, ok2)

	// Wrong token must NOT release the lock
	require.NoError(t, repoRedis.ReleaseCronLock(ctx, "trend_worker", "wrong-token"))
	_, ok3, err := repoRedis.AcquireCronLock(ctx, "trend_worker", time.Minute)
	require.NoError(t, err)
	assert.False(t, ok3)

	// Correct token releases the lock
	require.NoError(t, repoRedis.ReleaseCronLock(ctx, "trend_worker", tok))
	tok2, ok4, err := repoRedis.AcquireCronLock(ctx, "trend_worker", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok4)
	assert.NotEmpty(t, tok2)
}

func TestPubSubSubscription_StandaloneAndCluster(t *testing.T) {
	modes := []struct {
		name  string
		setup func(*testing.T) *test.FakeRedisServer
	}{
		{"Standalone", test.SetupTestRedis},
		{"Cluster", test.SetupTestRedisCluster},
	}

	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			srv := mode.setup(t)
			ctx := context.Background()

			sub := repoRedis.NewPubSubSubscription(ctx)
			require.NotNil(t, sub)
			defer func() { _ = sub.Close() }()

			channel := repoRedis.ChannelCommunity.Key(int64(42))
			require.NoError(t, sub.Subscribe(ctx, channel))
			ch := sub.Channel()
			time.Sleep(25 * time.Millisecond)

			require.NoError(t, repoRedis.ChannelCommunity.Publish(ctx, int64(42), []byte("hello-pubsub")))

			select {
			case msg, ok := <-ch:
				require.True(t, ok)
				assert.Equal(t, channel, msg.Channel)
				assert.Equal(t, "hello-pubsub", msg.Payload)
			case <-time.After(1 * time.Second):
				t.Fatalf("timed out waiting for pubsub message in %s mode", mode.name)
			}

			if mode.name == "Cluster" {
				assert.GreaterOrEqual(t, srv.SSubscribeCalls.Load(), int64(1))
				assert.GreaterOrEqual(t, srv.SPublishCalls.Load(), int64(1))
			}

			require.NoError(t, sub.Unsubscribe(ctx, channel))
			require.NoError(t, sub.Close())
		})
	}
}

func TestScanKeysAndCoreListOps(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	require.NoError(t, repoRedis.LPushOnCore(ctx, "raw_queue_test", "item_1", "item_2"))
	qLen, err := repoRedis.LLenOnCore(ctx, "raw_queue_test")
	require.NoError(t, err)
	assert.Equal(t, int64(2), qLen)

	popped, err := repoRedis.LPopOnCore(ctx, "raw_queue_test")
	require.NoError(t, err)
	assert.Equal(t, "item_2", popped)

	blpopped, err := repoRedis.BLPopOnCore(ctx, "raw_queue_test", time.Second)
	require.NoError(t, err)
	assert.Equal(t, "item_1", blpopped)

	// ScanKeys on Core & Objects
	require.NoError(t, repoRedis.Users.SetPrimitive(ctx, int64(500), "val"))
	keys, err := repoRedis.ScanKeysOnObjects(ctx, "object_cache:user:*", 100)
	require.NoError(t, err)
	assert.Contains(t, keys, "object_cache:user:500")

	// Cancelled context returns context.Canceled
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = repoRedis.ScanKeysOnCore(canceledCtx, "*", 100)
	assert.ErrorIs(t, err, context.Canceled)
}
