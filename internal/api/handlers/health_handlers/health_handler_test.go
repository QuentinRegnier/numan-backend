package health_handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLivenessHandler_AlwaysReturns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/healthz", LivenessHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"status":"alive"}`, w.Body.String())
}

func TestReadinessHandler_AllHealthy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetShuttingDown(false)
	t.Cleanup(func() { SetShuttingDown(false) })

	r := gin.New()
	r.GET("/readyz", NewReadinessHandler(ReadinessChecker{
		CheckRedis:    func(context.Context) error { return nil },
		CheckPostgres: func(context.Context) error { return nil },
		CheckMongo:    func(context.Context) error { return nil },
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "ready", body["status"])
}

func TestReadinessHandler_DependencyFailureReturns503WithoutLeakingInternalErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetShuttingDown(false)
	t.Cleanup(func() { SetShuttingDown(false) })

	r := gin.New()
	r.GET("/readyz", NewReadinessHandler(ReadinessChecker{
		CheckRedis:    func(context.Context) error { return nil },
		CheckPostgres: func(context.Context) error { return errors.New("pq: secret password auth failed") },
		CheckMongo:    func(context.Context) error { return nil },
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.NotContains(t, w.Body.String(), "secret password")
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "not_ready", body.Status)
	assert.Equal(t, "ok", body.Checks["redis"])
	assert.Equal(t, "error", body.Checks["postgres"])
	assert.Equal(t, "ok", body.Checks["mongo"])
}

func TestReadinessHandler_ShuttingDownReturns503Immediately(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetShuttingDown(true)
	t.Cleanup(func() { SetShuttingDown(false) })

	called := false
	r := gin.New()
	r.GET("/readyz", NewReadinessHandler(ReadinessChecker{
		CheckRedis: func(context.Context) error {
			called = true
			return nil
		},
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.False(t, called)
	assert.JSONEq(t, `{"status":"shutting_down"}`, w.Body.String())
}

func TestHealthHandlers_ExposeNodeIdentityWhenConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetShuttingDown(false)
	SetNodeIdentity("de-fra", "ws_node", 192)
	t.Cleanup(func() {
		SetNodeIdentity("", "", 0)
		SetShuttingDown(false)
	})

	r := gin.New()
	r.GET("/healthz", LivenessHandler)
	r.GET("/readyz", NewReadinessHandler(ReadinessChecker{
		CheckRedis:    func(context.Context) error { return nil },
		CheckPostgres: func(context.Context) error { return nil },
		CheckMongo:    func(context.Context) error { return nil },
	}))

	wHealth := httptest.NewRecorder()
	r.ServeHTTP(wHealth, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, wHealth.Code)
	var healthBody map[string]any
	require.NoError(t, json.Unmarshal(wHealth.Body.Bytes(), &healthBody))
	assert.Equal(t, "alive", healthBody["status"])
	assert.Equal(t, "de-fra", healthBody["datacenter"])
	assert.Equal(t, "ws_node", healthBody["role"])
	assert.Equal(t, float64(192), healthBody["node_id"])

	wReady := httptest.NewRecorder()
	r.ServeHTTP(wReady, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusOK, wReady.Code)
	var readyBody map[string]any
	require.NoError(t, json.Unmarshal(wReady.Body.Bytes(), &readyBody))
	assert.Equal(t, "ready", readyBody["status"])
	assert.Equal(t, "de-fra", readyBody["datacenter"])
	assert.Equal(t, "ws_node", readyBody["role"])
	assert.Equal(t, float64(192), readyBody["node_id"])
}

func TestReadinessHandler_PostgresReadReplicaDegradedStillReturns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetShuttingDown(false)
	t.Cleanup(func() { SetShuttingDown(false) })

	r := gin.New()
	r.GET("/readyz", NewReadinessHandler(ReadinessChecker{
		CheckRedis:        func(context.Context) error { return nil },
		CheckPostgres:     func(context.Context) error { return nil },
		CheckPostgresRead: func(context.Context) error { return errors.New("replica restarting during failover") },
		CheckMongo:        func(context.Context) error { return nil },
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "replica restarting")
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "ready", body.Status)
	assert.Equal(t, "ok", body.Checks["postgres"])
	assert.Equal(t, "degraded", body.Checks["postgres_read"])
	assert.Equal(t, "ok", body.Checks["redis"])
	assert.Equal(t, "ok", body.Checks["mongo"])
}
