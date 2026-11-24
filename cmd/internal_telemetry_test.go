package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/infra_telemetry_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/metrics"
	"github.com/QuentinRegnier/numan-backend/internal/service/infra_telemetry_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInternalMetricsMux_CLITelemetryAndSchedulerControl(t *testing.T) {
	_ = test.SetupTestRedis(t)

	cfg := scheduler_service.DefaultRegimeConfig()
	cfg.DownshiftHysteresis = 0
	eng := scheduler_service.NewEngine("fr-par", cfg, time.Now)
	eng.Register(scheduler_service.JobDefinition{
		Name:      "cli_trigger_demo",
		Category:  "hygiene",
		MinRegime: scheduler_models.RegimeDeepIdle,
		LockScope: scheduler_models.LockScopeLocalDC,
		Fn: func(_ context.Context, cp scheduler_models.JobCheckpoint, _ bool) (scheduler_models.JobCheckpoint, bool, error) {
			return cp, true, nil
		},
	})
	scheduler_service.SetDefault(eng)
	defer scheduler_service.SetDefault(nil)

	require.NoError(t, infra_telemetry_service.PublishPodHeartbeatOnce(context.Background(), infra_telemetry_service.HeartbeatConfig{
		DatacenterID: "fr-par",
		PodName:      "scheduler-0",
		Hostname:     "par-srv-01",
		Role:         "scheduler_node",
	}))
	_, err := infra_telemetry_service.RunTelemetryAggregationOnce(context.Background(), infra_telemetry_service.AggregatorConfig{
		DatacenterID: "fr-par",
		Scheduler:    eng,
	})
	require.NoError(t, err)

	mux := buildInternalMetricsMux("fr-par")

	// 1. GET /internal/cli-telemetry (Zero-JWT K8s API proxy endpoint)
	{
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/cli-telemetry", nil))
		require.Equal(t, http.StatusOK, w.Code)

		var snap infra_telemetry_models.CLITelemetrySnapshot
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snap))
		assert.Equal(t, "fr-par", snap.Datacenter)
		assert.NotEmpty(t, snap.Servers)
		assert.Equal(t, "standalone", snap.RedisTiers.Core.Mode)
	}

	// 2. POST /internal/scheduler/pause -> immediately pauses engine
	{
		req := httptest.NewRequest(http.MethodPost, "/internal/scheduler/pause", bytes.NewBufferString(`{"ttl_minutes":20,"reason":"k8s_node_drain"}`))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		paused, reason := eng.IsPaused(context.Background())
		assert.True(t, paused)
		assert.Equal(t, "k8s_node_drain", reason)
	}

	// 3. POST /internal/scheduler/resume -> lifts pause
	{
		req := httptest.NewRequest(http.MethodPost, "/internal/scheduler/resume", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		paused, _ := eng.IsPaused(context.Background())
		assert.False(t, paused)
	}

	// 4. POST /internal/scheduler/jobs/trigger -> triggers registered job via JSON body
	{
		req := httptest.NewRequest(http.MethodPost, "/internal/scheduler/jobs/trigger", bytes.NewBufferString(`{"job_name":"cli_trigger_demo","force":true}`))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var out admin_models.SchedulerActionOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		assert.Equal(t, "executed", out.Status)
		require.NotNil(t, out.Job)
		assert.Equal(t, "completed", out.Job.Status)
	}

	// 5. GET /metrics exposes Prometheus text format
	{
		metrics.Default().SetSchedulerMetricsProvider(eng.MetricsSnapshot)
		defer metrics.Default().SetSchedulerMetricsProvider(nil)

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "numan_process_uptime_seconds")
		assert.Contains(t, w.Body.String(), "numan_scheduler_regime")
	}
}
