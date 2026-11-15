package telemetry_handlers_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/api/handlers/telemetry_handlers"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTelemetryRouter(userID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if userID > 0 {
			c.Set("userID", userID)
		}
		c.Next()
	})
	r.PATCH("/sync/telemetry", telemetry_handlers.SyncTelemetryHandler)
	return r
}

func TestSyncTelemetryHandler_InvalidPayload_NoRedisWrites(t *testing.T) {
	_ = test.SetupTestRedis(t)
	r := setupTelemetryRouter(42)

	// Bad vector length (2 instead of 224)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/sync/telemetry", bytes.NewBufferString(`{"schema_version":1,"vector":{"dims":2,"values":[0.1,0.2]},"meta":{"confidence_score":0.2,"client_updated_at":1000}}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Negative dwell_time_ms
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/sync/telemetry", bytes.NewBufferString(`{"schema_version":1,"meta":{"confidence_score":0.2,"client_updated_at":1000},"telemetry":[{"post_id":1,"dwell_time_ms":-10}]}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	keys, errKeys := infraRedis.Rdb.Keys(context.Background(), "*").Result()
	require.NoError(t, errKeys)
	assert.Empty(t, keys)
}
