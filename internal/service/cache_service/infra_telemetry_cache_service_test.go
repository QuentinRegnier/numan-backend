package cache_service_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/infra_telemetry_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInfraTelemetryCacheService(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	t.Run("pod heartbeat round-trip and list scan", func(t *testing.T) {
		hb := infra_telemetry_models.PodHeartbeat{
			Datacenter:         "dc-eu-1",
			PodName:            "api-0",
			Hostname:           "srv-1",
			Role:               "api_node",
			HTTPRequestsPerSec: 120.5,
			TimestampMs:        time.Now().UnixMilli(),
		}
		require.NoError(t, cache_service.SavePodHeartbeat(ctx, hb, 30*time.Second))

		list, err := cache_service.ListActivePodHeartbeats(ctx, "dc-eu-1")
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, "api-0", list[0].PodName)
		assert.InDelta(t, 120.5, list[0].HTTPRequestsPerSec, 0.01)
	})

	t.Run("live snapshot, hourly profile, geoip bucket and history ring", func(t *testing.T) {
		snap := &infra_telemetry_models.CLITelemetrySnapshot{
			Datacenter:      "dc-eu-1",
			TimestampMs:     time.Now().UnixMilli(),
			SchedulerRegime: scheduler_models.RegimeIdle,
		}
		require.NoError(t, cache_service.SaveLiveTelemetrySnapshot(ctx, "dc-eu-1", snap, time.Minute))
		got, err := cache_service.GetLiveTelemetrySnapshot(ctx, "dc-eu-1")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, scheduler_models.RegimeIdle, got.SchedulerRegime)

		var prof infra_telemetry_models.HourlyProfile
		prof.Hours[3] = 0.12
		prof.SampleCount[3] = 5
		cache_service.SaveHourlyProfile(ctx, "dc-eu-1", prof, time.Hour)
		gotProf := cache_service.GetHourlyProfile(ctx, "dc-eu-1")
		assert.InDelta(t, 0.12, gotProf.Hours[3], 0.001)

		bucket := infra_telemetry_models.GeoIPBucket{
			UpdatedAtMs: time.Now().UnixMilli(),
			Countries: map[string]infra_telemetry_models.CountryDelta{
				"FR": {Requests: 42, DurationMsSum: 840},
			},
		}
		cache_service.SaveGeoIPBucket(ctx, "dc-eu-1", bucket, time.Hour)
		gotBucket := cache_service.GetGeoIPBucket(ctx, "dc-eu-1")
		assert.Equal(t, int64(42), gotBucket.Countries["FR"].Requests)

		hist := cache_service.AppendTelemetryHistory1m(ctx, "dc-eu-1", infra_telemetry_models.HistorySample1m{
			TimestampMs: time.Now().UnixMilli(),
			Activity:    0.25,
			Saturation:  0.30,
			RPS:         50,
			Regime:      "IDLE",
		}, 1440, 60)
		require.Len(t, hist, 1)
		assert.Equal(t, "IDLE", hist[0].Regime)
	})

	t.Run("aggregator lock and 3-tier redis snapshot", func(t *testing.T) {
		tok, ok, err := cache_service.AcquireTelemetryAggregatorLock(ctx, 10*time.Second)
		require.NoError(t, err)
		require.True(t, ok)
		require.NotEmpty(t, tok)
		cache_service.ReleaseTelemetryAggregatorLock(ctx, tok)

		tiers := cache_service.CollectRedis3TierSnapshot(ctx)
		assert.NotEmpty(t, tiers.Core.Mode)
		assert.True(t, tiers.ObjectPool.SharedWithCore)
		assert.True(t, tiers.PubSub.SharedWithCore)

		depth, age, dlq := cache_service.CollectWriteBehindQueueStats(ctx)
		assert.GreaterOrEqual(t, depth, int64(0))
		assert.GreaterOrEqual(t, age, 0.0)
		assert.GreaterOrEqual(t, dlq, int64(0))
	})
}
