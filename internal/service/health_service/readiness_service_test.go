package health_service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/service/health_service"
	"github.com/stretchr/testify/assert"
)

func TestCheckReadiness(t *testing.T) {
	t.Cleanup(func() {
		health_service.SetNodeIdentity("", "", 0)
		health_service.SetShuttingDown(false)
	})

	okFn := func(context.Context) error { return nil }
	errFn := func(context.Context) error { return errors.New("boom") }

	t.Run("returns ready when all critical dependencies succeed and object pool degrades gracefully", func(t *testing.T) {
		health_service.SetNodeIdentity("dc-eu-1", "api_node", 1)
		out, ready := health_service.CheckReadiness(context.Background(), health_service.ReadinessChecker{
			CheckRedis:        okFn,
			CheckRedisObjects: errFn,
			CheckRedisPubSub:  errFn, // non-critical for api_node
			CheckPostgres:     okFn,
			CheckPostgresRead: errFn,
			CheckMongo:        okFn,
		})
		assert.True(t, ready)
		assert.Equal(t, "ready", out.Status)
		assert.Equal(t, "degraded", out.Checks["redis_objects"])
		assert.Equal(t, "degraded", out.Checks["redis_pubsub"])
		assert.Equal(t, "degraded", out.Checks["postgres_read"])
	})

	t.Run("fails when redis core or pubsub on ws_node fails", func(t *testing.T) {
		health_service.SetNodeIdentity("dc-eu-1", "ws_node", 2)
		out, ready := health_service.CheckReadiness(context.Background(), health_service.ReadinessChecker{
			CheckRedis:       okFn,
			CheckRedisPubSub: errFn,
			CheckPostgres:    okFn,
			CheckMongo:       okFn,
		})
		assert.False(t, ready)
		assert.Equal(t, "not_ready", out.Status)
		assert.Equal(t, "error", out.Checks["redis_pubsub"])
	})

	t.Run("returns shutting_down during graceful drain", func(t *testing.T) {
		health_service.SetShuttingDown(true)
		out, ready := health_service.CheckReadiness(context.Background(), health_service.ReadinessChecker{CheckRedis: okFn})
		assert.False(t, ready)
		assert.Equal(t, "shutting_down", out.Status)
	})
}
