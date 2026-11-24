package health_handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/health_models"
	"github.com/QuentinRegnier/numan-backend/internal/service/health_service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadinessHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() {
		health_service.SetNodeIdentity("", "", 0)
		health_service.SetShuttingDown(false)
	})

	okFn := func(context.Context) error { return nil }
	errFn := func(context.Context) error { return errors.New("down") }

	t.Run("200 OK when critical tiers pass and optional object pool degrades", func(t *testing.T) {
		health_service.SetNodeIdentity("dc-eu-1", "api_node", 1)
		r := gin.New()
		r.GET("/readyz", NewReadinessHandler(health_service.ReadinessChecker{
			CheckRedis:        okFn,
			CheckRedisObjects: errFn,
			CheckPostgres:     okFn,
			CheckMongo:        okFn,
		}))

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		require.Equal(t, http.StatusOK, w.Code)

		var out health_models.ReadinessOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		assert.Equal(t, "ready", out.Status)
		assert.Equal(t, "degraded", out.Checks["redis_objects"])
	})

	t.Run("503 Service Unavailable when redis core fails", func(t *testing.T) {
		r := gin.New()
		r.GET("/readyz", NewReadinessHandler(health_service.ReadinessChecker{
			CheckRedis:    errFn,
			CheckPostgres: okFn,
			CheckMongo:    okFn,
		}))

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		require.Equal(t, http.StatusServiceUnavailable, w.Code)

		var out health_models.ReadinessOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		assert.Equal(t, "not_ready", out.Status)
		assert.Equal(t, "error", out.Checks["redis_core"])
	})
}
