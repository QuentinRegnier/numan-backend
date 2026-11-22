package metrics

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRegistry_RenderPrometheusExpositionFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg := NewRegistry()

	reg.SetWSStatsProvider(func() (int64, int64) { return 12, 19 })
	reg.SetPostgresStatsProvider(func() (sql.DBStats, bool) {
		return sql.DBStats{OpenConnections: 10, InUse: 4, Idle: 6, WaitCount: 3}, true
	})
	reg.SetPostgresReadStatsProvider(func() (sql.DBStats, bool) {
		return sql.DBStats{OpenConnections: 18, InUse: 7, Idle: 11, WaitCount: 1}, true
	})
	reg.SetQueueDepthProvider(func(context.Context) (int64, bool) { return 42, true })
	reg.SetRedisEvictedKeysProvider(func(context.Context) (int64, bool) { return 5, true })
	reg.IncWorkerBatch(128)
	reg.IncWorkerBatch(0) // ignoré

	r := gin.New()
	r.Use(reg.GinMiddleware())
	r.GET("/api/v12/posts/:id", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// 2 requêtes sur la même route paramétrée -> même label path="/api/v12/posts/:id" (pas d'explosion de cardinalité)
	for _, id := range []string{"101", "202"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v12/posts/"+id, nil))
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// Route inconnue -> label path="unmatched"
	w404 := httptest.NewRecorder()
	r.ServeHTTP(w404, httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))
	assert.Equal(t, http.StatusNotFound, w404.Code)

	mw := httptest.NewRecorder()
	reg.Handler().ServeHTTP(mw, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	assert.Equal(t, http.StatusOK, mw.Code)
	assert.Contains(t, mw.Header().Get("Content-Type"), "text/plain; version=0.0.4")
	body := mw.Body.String()

	assert.Contains(t, body, `numan_http_requests_total{method="GET",path="/api/v12/posts/:id",status="200"} 2`)
	assert.Contains(t, body, `numan_http_requests_total{method="GET",path="unmatched",status="404"} 1`)
	assert.Contains(t, body, "numan_http_requests_in_flight 0")
	assert.Contains(t, body, "numan_worker_batches_processed_total 1")
	assert.Contains(t, body, "numan_worker_events_processed_total 128")
	assert.Contains(t, body, "numan_worker_redis_queue_depth 42")
	assert.Contains(t, body, "numan_redis_evicted_keys_total 5")
	assert.Contains(t, body, "numan_websocket_users_active 12")
	assert.Contains(t, body, "numan_websocket_connections_active 19")
	assert.Contains(t, body, "numan_db_postgres_open_connections 10")
	assert.Contains(t, body, "numan_db_postgres_in_use 4")
	assert.Contains(t, body, "numan_db_postgres_idle 6")
	assert.Contains(t, body, "numan_db_postgres_wait_count_total 3")
	assert.Contains(t, body, "numan_postgres_write_open_connections 10")
	assert.Contains(t, body, "numan_postgres_read_open_connections 18")
	assert.Contains(t, body, "numan_postgres_read_in_use 7")
	assert.Contains(t, body, "numan_postgres_read_idle 11")
	assert.Contains(t, body, "numan_postgres_read_wait_count_total 1")
	assert.Contains(t, body, "numan_go_goroutines ")
	assert.Contains(t, body, "numan_go_mem_alloc_bytes ")
}
