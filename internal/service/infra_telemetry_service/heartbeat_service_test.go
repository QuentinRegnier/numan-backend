package infra_telemetry_service_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/metrics"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/infra_telemetry_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPodHeartbeatPublishAndCollect(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	metrics.Default().RecordHTTPDuration(42.5)
	metrics.Default().RecordCountryRequest("FR", 42.5)
	metrics.Default().SetWorkerConcurrency(8)

	err := infra_telemetry_service.PublishPodHeartbeatOnce(ctx, infra_telemetry_service.HeartbeatConfig{
		DatacenterID: "dc-eu-1",
		PodName:      "api-node-1",
		Hostname:     "srv-1",
		Role:         "api_node",
		NodeID:       1,
		TTL:          30 * time.Second,
		WSStatsFn:    func() (int64, int64) { return 120, 150 },
		CuckooFillFn: func() (float64, bool) { return 0.35, true },
	})
	require.NoError(t, err)

	pods, err := cache_service.ListActivePodHeartbeats(ctx, "dc-eu-1")
	require.NoError(t, err)
	require.Len(t, pods, 1)
	assert.Equal(t, "api-node-1", pods[0].PodName)
	assert.Equal(t, int64(150), pods[0].WSActiveConnections)
	assert.InDelta(t, 0.35, pods[0].CuckooFillRatio, 0.001)
}
