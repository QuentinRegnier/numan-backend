package infra_telemetry_service_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/infra_telemetry_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/infra_telemetry_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunTelemetryAggregationOnceAndGeoIPAdvisor(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	// Enregistrer un heartbeat simulé avec trafic US à latence élevée (>150ms)
	require.NoError(t, cache_service.SavePodHeartbeat(ctx, infra_telemetry_models.PodHeartbeat{
		Datacenter:         "dc-eu-1",
		PodName:            "api-0",
		Hostname:           "srv-eu-1",
		Role:               "api_node",
		HTTPRequestsPerSec: 250,
		HTTPP95LatencyMs:   180,
		GoCPUUsageRatio:    0.30,
		TimestampMs:        time.Now().UnixMilli(),
		GeoIPCountryCountsDelta: map[string]infra_telemetry_models.CountryDelta{
			"us": {Requests: 80, DurationMsSum: 80 * 210.0},
			"FR": {Requests: 20, DurationMsSum: 20 * 25.0},
		},
	}, time.Minute))

	cfg := scheduler_service.DefaultRegimeConfig()
	cfg.DownshiftHysteresis = 0
	eng := scheduler_service.NewEngine("dc-eu-1", cfg, nil)

	snap, err := infra_telemetry_service.RunTelemetryAggregationOnce(ctx, infra_telemetry_service.AggregatorConfig{
		DatacenterID: "dc-eu-1",
		Location:     time.UTC,
		Scheduler:    eng,
		MaxRPSRef:    2000,
	})
	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.NoError(t, snap.Validate())
	assert.Equal(t, "dc-eu-1", snap.Datacenter)
	require.Len(t, snap.Servers, 1)
	assert.Equal(t, "srv-eu-1", snap.Servers[0].Hostname)
	require.Len(t, snap.GeoIP24h.TopCountries, 2)
	assert.Equal(t, "US", snap.GeoIP24h.TopCountries[0].CountryCode)
	assert.Contains(t, snap.GeoIP24h.ExpansionSuggestion, "US")

	// Lecture <1ms depuis Redis Core
	live, err := infra_telemetry_service.GetLiveTelemetrySnapshot(ctx, "dc-eu-1")
	require.NoError(t, err)
	require.NotNil(t, live)
	assert.Equal(t, snap.TimestampMs, live.TimestampMs)

	// Vérifier que les métriques 3-tier Prometheus sont bien produites
	tierMetrics := infra_telemetry_service.CollectRedis3TierMetrics(ctx)
	require.Len(t, tierMetrics, 3)

	// Vérifier que le régime OVERLOAD est détecté sur saturation élevée
	require.NoError(t, cache_service.SavePodHeartbeat(ctx, infra_telemetry_models.PodHeartbeat{
		Datacenter:            "dc-eu-1",
		PodName:               "api-0",
		Hostname:              "srv-eu-1",
		Role:                  "api_node",
		PostgresWritePoolUtil: 0.95,
		TimestampMs:           time.Now().UnixMilli(),
	}, time.Minute))
	snap2, err := infra_telemetry_service.RunTelemetryAggregationOnce(ctx, infra_telemetry_service.AggregatorConfig{
		DatacenterID: "dc-eu-1",
		Scheduler:    eng,
	})
	require.NoError(t, err)
	assert.Equal(t, scheduler_models.RegimeOverload, snap2.SchedulerRegime)
}
