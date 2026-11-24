package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/infra_telemetry_models"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtendedMetrics_GeoIPAndRender(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg := NewRegistry()
	reg.SetDatacenter("dc1")

	r := gin.New()
	r.Use(reg.GinMiddleware())
	r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	for _, hdr := range []string{"fr", "US", "192.168.1.50"} {
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.Header.Set("CF-IPCountry", hdr)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	delta := reg.DrainGeoIPDelta()
	require.Contains(t, delta, "FR")
	require.Contains(t, delta, "US")
	require.Contains(t, delta, "ZZ")
	assert.NotContains(t, delta, "192.168.1.50")
	assert.Empty(t, reg.DrainGeoIPDelta())

	reg.SetWorkerConcurrency(8)
	reg.IncWorkerActive()
	reg.IncWorkerActive()
	cfg, active, sat := reg.WorkerSaturation()
	assert.Equal(t, int64(8), cfg)
	assert.Equal(t, int64(2), active)
	assert.InDelta(t, 0.25, sat, 0.001)
	reg.DecWorkerActive()

	reg.SetQueueDepthProvider(func(ctx context.Context) (int64, bool) { return 42, true })
	reg.SetQueueOldestAgeProvider(func(ctx context.Context) (float64, bool) { return 1.5, true })
	reg.SetDLQSizeProvider(func(ctx context.Context) (int64, bool) { return 3, true })
	reg.SetCuckooFillProvider(func() (float64, bool) { return 0.42, true })
	reg.SetDCSaturationProvider(func() (string, float64, bool) { return "dc1", 0.31, true })
	reg.SetRedis3TierProvider(func(ctx context.Context) []infra_telemetry_models.RedisTierMetricSnapshot {
		return []infra_telemetry_models.RedisTierMetricSnapshot{
			{
				Tier:        "core",
				Mode:        "cluster",
				EvictedKeys: 2,
				Shards: []infra_telemetry_models.RedisShardMetric{
					{Tier: "core", ShardID: "core-0", Role: "master", Node: "srv-1", UsedMemoryBytes: 1024, MaxMemoryBytes: 4096, FillRatio: 0.25},
				},
			},
			{Tier: "pubsub", Mode: "standalone", PubSubChannels: 15, PubSubMsgPerSec: 120.5},
		}
	})
	reg.SetSchedulerMetricsProvider(func() (infra_telemetry_models.SchedulerMetricSnapshot, bool) {
		return infra_telemetry_models.SchedulerMetricSnapshot{
			Datacenter:      "dc1",
			Regime:          "IDLE",
			StarvationAlert: false,
			Jobs: []infra_telemetry_models.SchedulerJobMetric{
				{Name: "hashtag_canon", Running: true, LastDurationSec: 1.25},
			},
		}, true
	})

	rendered := reg.Render(context.Background())
	assert.Contains(t, rendered, `numan_http_requests_by_country_total{datacenter="dc1",country="FR"} 1`)
	assert.NotContains(t, rendered, "192.168.1.50")
	assert.Contains(t, rendered, `numan_worker_concurrency_configured 8`)
	assert.Contains(t, rendered, `numan_worker_queue_depth_total 42`)
	assert.Contains(t, rendered, `numan_worker_queue_oldest_event_age_seconds 1.500`)
	assert.Contains(t, rendered, `numan_dlq_size 3`)
	assert.Contains(t, rendered, `numan_redis_tier_mode{tier="core",mode="cluster"} 1`)
	assert.Contains(t, rendered, `numan_redis_pubsub_active_channels 15`)
	assert.Contains(t, rendered, `numan_scheduler_regime{datacenter="dc1",regime="IDLE"} 1`)
	assert.Contains(t, rendered, `numan_scheduler_job_running{job="hashtag_canon"} 1`)
	assert.Contains(t, rendered, `numan_dc_saturation_ratio{datacenter="dc1"} 0.3100`)
	assert.Contains(t, rendered, `numan_cuckoo_fill_ratio 0.4200`)
}
