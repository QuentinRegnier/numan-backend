package redis

import (
	"context"
	"crypto/tls"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearRedisEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"REDIS_ADDR", "REDIS_PASSWORD",
		"REDIS_MASTER_NAME", "REDIS_SENTINEL_ADDRS", "REDIS_SENTINEL_PASSWORD",
		"REDIS_TLS", "REDIS_TLS_SERVER_NAME",
		"REDIS_MAX_RETRIES", "REDIS_DIAL_TIMEOUT_SEC",
		"REDIS_READ_TIMEOUT_SEC", "REDIS_WRITE_TIMEOUT_SEC",
		"REDIS_POOL_SIZE", "REDIS_ROUTE_BY_LATENCY",
	} {
		t.Setenv(key, "")
	}
}

func TestResolveRedisOptions_DefaultsAndTLS(t *testing.T) {
	t.Run("defaults without TLS", func(t *testing.T) {
		clearRedisEnv(t)
		t.Setenv("REDIS_PASSWORD", "secret")

		opts := ResolveRedisOptions()
		assert.Equal(t, "redis:6379", opts.Addr)
		assert.Equal(t, "secret", opts.Password)
		assert.Nil(t, opts.TLSConfig)
		assert.Equal(t, 3, opts.MaxRetries)
		assert.Equal(t, 5*time.Second, opts.DialTimeout)
		assert.Equal(t, 3*time.Second, opts.ReadTimeout)
		assert.Equal(t, 3*time.Second, opts.WriteTimeout)
		assert.Equal(t, 50, opts.PoolSize)
	})

	t.Run("TLS enabled derives ServerName from host", func(t *testing.T) {
		clearRedisEnv(t)
		t.Setenv("REDIS_ADDR", "redis.numan.svc.cluster.local:6379")
		t.Setenv("REDIS_PASSWORD", "secret")
		t.Setenv("REDIS_TLS", "true")

		opts := ResolveRedisOptions()
		require.NotNil(t, opts.TLSConfig)
		assert.Equal(t, uint16(tls.VersionTLS12), opts.TLSConfig.MinVersion)
		assert.Equal(t, "redis.numan.svc.cluster.local", opts.TLSConfig.ServerName)
	})

	t.Run("TLS explicit ServerName override", func(t *testing.T) {
		clearRedisEnv(t)
		t.Setenv("REDIS_ADDR", "10.0.0.15:6379")
		t.Setenv("REDIS_TLS", "true")
		t.Setenv("REDIS_TLS_SERVER_NAME", "redis.internal")

		opts := ResolveRedisOptions()
		require.NotNil(t, opts.TLSConfig)
		assert.Equal(t, "redis.internal", opts.TLSConfig.ServerName)
	})
}

func TestResolveRedisConfig_SentinelHAAndFailoverOptions(t *testing.T) {
	clearRedisEnv(t)
	t.Setenv("REDIS_MASTER_NAME", "numan-master")
	t.Setenv("REDIS_SENTINEL_ADDRS", "redis-0.redis-headless:26379, redis-1.redis-headless:26379, redis-2.redis-headless:26379")
	t.Setenv("REDIS_PASSWORD", "master-secret")
	t.Setenv("REDIS_SENTINEL_PASSWORD", "sentinel-secret")
	t.Setenv("REDIS_TLS", "true")
	t.Setenv("REDIS_POOL_SIZE", "80")
	t.Setenv("REDIS_MAX_RETRIES", "5")
	t.Setenv("REDIS_DIAL_TIMEOUT_SEC", "4")
	t.Setenv("REDIS_READ_TIMEOUT_SEC", "2")
	t.Setenv("REDIS_WRITE_TIMEOUT_SEC", "2")

	cfg := ResolveRedisConfig()
	require.True(t, cfg.IsSentinel())
	assert.Equal(t, "numan-master", cfg.MasterName)
	assert.Equal(t, []string{
		"redis-0.redis-headless:26379",
		"redis-1.redis-headless:26379",
		"redis-2.redis-headless:26379",
	}, cfg.SentinelAddrs)
	assert.False(t, cfg.RouteByLatency, "RouteByLatency doit être désactivé par défaut pour garantir la cohérence des Tombstones et Write-Behind")

	failoverOpts := cfg.FailoverOptions()
	assert.Equal(t, "numan-master", failoverOpts.MasterName)
	assert.Equal(t, cfg.SentinelAddrs, failoverOpts.SentinelAddrs)
	assert.Equal(t, "master-secret", failoverOpts.Password)
	assert.Equal(t, "sentinel-secret", failoverOpts.SentinelPassword)
	assert.Equal(t, 80, failoverOpts.PoolSize)
	assert.Equal(t, 5, failoverOpts.MaxRetries)
	assert.Equal(t, 4*time.Second, failoverOpts.DialTimeout)
	assert.Equal(t, 2*time.Second, failoverOpts.ReadTimeout)
	assert.Equal(t, 2*time.Second, failoverOpts.WriteTimeout)
	require.NotNil(t, failoverOpts.TLSConfig)
	assert.Equal(t, "redis-0.redis-headless", failoverOpts.TLSConfig.ServerName)

	client := NewRedisClient(cfg)
	require.NotNil(t, client)
	assert.NoError(t, client.Close())
}

func TestResolveRedisConfig_StandaloneWhenSentinelAddrsEmpty(t *testing.T) {
	clearRedisEnv(t)
	t.Setenv("REDIS_ADDR", "redis:6379")
	t.Setenv("REDIS_MASTER_NAME", "numan-master")

	cfg := ResolveRedisConfig()
	assert.False(t, cfg.IsSentinel())
	client := NewRedisClient(cfg)
	require.NotNil(t, client)
	assert.NoError(t, client.Close())
}

func TestParseEvictedKeys_AndCollectNilSafe(t *testing.T) {
	info := "# Stats\r\ntotal_connections_received:42\r\nevicted_keys:17\r\nkeyspace_hits:100\r\n"
	evicted, ok := ParseEvictedKeys(info)
	assert.True(t, ok)
	assert.Equal(t, int64(17), evicted)

	_, ok = ParseEvictedKeys("# Stats\r\ntotal_connections_received:42\r\n")
	assert.False(t, ok)

	prev := Rdb
	Rdb = nil
	t.Cleanup(func() { Rdb = prev })
	_, ok = CollectEvictedKeys(context.Background())
	assert.False(t, ok)
}

func TestCloseRedis_NilSafe(t *testing.T) {
	prev := Rdb
	prevCore := RdbCore
	prevObj := RdbObjects
	prevPub := RdbPubSub
	Rdb = nil
	RdbCore = nil
	RdbObjects = nil
	RdbPubSub = nil
	t.Cleanup(func() {
		Rdb = prev
		RdbCore = prevCore
		RdbObjects = prevObj
		RdbPubSub = prevPub
	})
	assert.NoError(t, CloseRedis())
}

func TestResolveTierConfig_ClusterAndZeroOverheadCollapse(t *testing.T) {
	clearRedisEnv(t)
	for _, key := range []string{
		"REDIS_CLUSTER_ADDRS",
		"REDIS_OBJECT_ADDR", "REDIS_OBJECT_CLUSTER_ADDRS", "REDIS_OBJECT_SENTINEL_ADDRS",
		"REDIS_PUBSUB_ADDR", "REDIS_PUBSUB_CLUSTER_ADDRS", "REDIS_PUBSUB_SENTINEL_ADDRS",
	} {
		t.Setenv(key, "")
	}

	// Zero-overhead collapse: REDIS_OBJECT_* and REDIS_PUBSUB_* unset => not configured
	objCfg := ResolveTierConfig("REDIS_OBJECT")
	pubCfg := ResolveTierConfig("REDIS_PUBSUB")
	assert.False(t, objCfg.IsConfigured())
	assert.False(t, pubCfg.IsConfigured())

	coreCfg := ResolveTierConfig("REDIS")
	assert.True(t, coreCfg.IsConfigured())
	assert.Equal(t, TopologyModeStandalone, coreCfg.Mode())

	// Simulate collapsed tier wiring
	coreClient := NewRedisClient(coreCfg)
	t.Cleanup(func() { _ = coreClient.Close() })

	prevRdb, prevCore, prevObj, prevPub := Rdb, RdbCore, RdbObjects, RdbPubSub
	t.Cleanup(func() {
		Rdb, RdbCore, RdbObjects, RdbPubSub = prevRdb, prevCore, prevObj, prevPub
	})
	Rdb = coreClient
	RdbCore = coreClient
	RdbObjects = coreClient
	RdbPubSub = coreClient

	assert.Equal(t, CoreClient(), ObjectClient(), "when REDIS_OBJECT_* is unset, ObjectClient must alias CoreClient")
	assert.Equal(t, CoreClient(), PubSubClient(), "when REDIS_PUBSUB_* is unset, PubSubClient must alias CoreClient")
	assert.False(t, HasDedicatedObjectPool())
	assert.False(t, HasDedicatedPubSub())
	assert.False(t, IsClusterClient(CoreClient()))
	assert.Equal(t, TopologyModeStandalone, TierTopologyMode(CoreClient()))

	// Cluster mode resolution for Tier 2 Object Pool
	t.Setenv("REDIS_OBJECT_CLUSTER_ADDRS", "obj-0:6379, obj-1:6379, obj-2:6379")
	t.Setenv("REDIS_OBJECT_ROUTE_BY_LATENCY", "true")
	objClusterCfg := ResolveTierConfig("REDIS_OBJECT")
	require.True(t, objClusterCfg.IsCluster())
	assert.Equal(t, TopologyModeCluster, objClusterCfg.Mode())
	assert.True(t, objClusterCfg.RouteByLatency)
	assert.Equal(t, []string{"obj-0:6379", "obj-1:6379", "obj-2:6379"}, objClusterCfg.ClusterAddrs)

	objClusterClient := NewRedisClient(objClusterCfg)
	t.Cleanup(func() { _ = objClusterClient.Close() })
	assert.True(t, IsClusterClient(objClusterClient))
	assert.Equal(t, TopologyModeCluster, TierTopologyMode(objClusterClient))

	RdbObjects = objClusterClient
	assert.True(t, HasDedicatedObjectPool())
	assert.NotEqual(t, CoreClient(), ObjectClient())
}
