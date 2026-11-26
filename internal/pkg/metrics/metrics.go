package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

type httpMetricKey struct {
	method string
	path   string
	status string
}

// Registry centralise les métriques applicatives exposées au format Prometheus sur le port interne /metrics.
type Registry struct {
	startTime time.Time

	httpMu       sync.RWMutex
	httpCounters map[httpMetricKey]*atomic.Uint64
	httpInFlight atomic.Int64

	workerBatches atomic.Uint64
	workerEvents  atomic.Uint64

	providerMu     sync.RWMutex
	wsStatsFn      func() (activeUsers int64, activeConnections int64)
	pgStatsFn      func() (sql.DBStats, bool)
	pgReadStatsFn  func() (sql.DBStats, bool)
	queueDepthFn   func(ctx context.Context) (int64, bool)
	redisEvictedFn func(ctx context.Context) (int64, bool)

	ext    *extendedState
	orphan *orphanCleanerMetricsState
}

var defaultRegistry = NewRegistry()

// NewRegistry crée un registre de métriques isolé (utile pour les tests unitaires).
func NewRegistry() *Registry {
	return &Registry{
		startTime:    time.Now(),
		httpCounters: make(map[httpMetricKey]*atomic.Uint64),
		ext:          newExtendedState(),
		orphan:       newOrphanCleanerMetricsState(),
	}
}

// Default retourne le registre global de métriques du processus.
func Default() *Registry {
	return defaultRegistry
}

// SetWSStatsProvider enregistre la sonde de statistiques du Hub WebSocket.
func (r *Registry) SetWSStatsProvider(fn func() (activeUsers int64, activeConnections int64)) {
	r.providerMu.Lock()
	r.wsStatsFn = fn
	r.providerMu.Unlock()
}

// SetPostgresStatsProvider enregistre la sonde du pool sql.DB PostgreSQL Primary (Write).
func (r *Registry) SetPostgresStatsProvider(fn func() (sql.DBStats, bool)) {
	r.providerMu.Lock()
	r.pgStatsFn = fn
	r.providerMu.Unlock()
}

// SetPostgresReadStatsProvider enregistre la sonde du pool sql.DB PostgreSQL Read Replica.
func (r *Registry) SetPostgresReadStatsProvider(fn func() (sql.DBStats, bool)) {
	r.providerMu.Lock()
	r.pgReadStatsFn = fn
	r.providerMu.Unlock()
}

// SetQueueDepthProvider enregistre la sonde de profondeur des files Redis Write-Behind.
func (r *Registry) SetQueueDepthProvider(fn func(ctx context.Context) (int64, bool)) {
	r.providerMu.Lock()
	r.queueDepthFn = fn
	r.providerMu.Unlock()
}

// SetRedisEvictedKeysProvider enregistre la sonde du compteur d'évictions Redis (surveillance Tombstones volatile-lfu).
func (r *Registry) SetRedisEvictedKeysProvider(fn func(ctx context.Context) (int64, bool)) {
	r.providerMu.Lock()
	r.redisEvictedFn = fn
	r.providerMu.Unlock()
}

// RecordHTTPRequest incrémente le compteur HTTP pour un triplet (method, route, status).
func (r *Registry) RecordHTTPRequest(method, path string, statusCode int) {
	if path == "" {
		path = "unmatched"
	}
	key := httpMetricKey{
		method: method,
		path:   path,
		status: strconv.Itoa(statusCode),
	}

	r.httpMu.RLock()
	counter, ok := r.httpCounters[key]
	r.httpMu.RUnlock()
	if ok {
		counter.Add(1)
		return
	}

	r.httpMu.Lock()
	counter, ok = r.httpCounters[key]
	if !ok {
		counter = &atomic.Uint64{}
		r.httpCounters[key] = counter
	}
	r.httpMu.Unlock()
	counter.Add(1)
}

// IncWorkerBatch enregistre un lot Write-Behind traité avec succès.
func (r *Registry) IncWorkerBatch(eventsCount int) {
	if eventsCount <= 0 {
		return
	}
	r.workerBatches.Add(1)
	r.workerEvents.Add(uint64(eventsCount))
}

// IncWorkerBatch incrémente les compteurs Write-Behind sur le registre global.
func IncWorkerBatch(eventsCount int) {
	defaultRegistry.IncWorkerBatch(eventsCount)
}

// GinMiddleware instrumente toutes les requêtes HTTP Gin en utilisant la route normalisée (c.FullPath).
func GinMiddleware() gin.HandlerFunc {
	return defaultRegistry.GinMiddleware()
}

// GinMiddleware instrumente les requêtes HTTP sur ce registre spécifique
// (compteurs, latence p95, et agrégation GeoIP privacy-safe sans stockage d'IP).
func (r *Registry) GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		r.httpInFlight.Add(1)
		defer r.httpInFlight.Add(-1)

		c.Next()
		durMs := float64(time.Since(start).Microseconds()) / 1000.0
		r.RecordHTTPRequest(c.Request.Method, c.FullPath(), c.Writer.Status())
		r.RecordHTTPDuration(durMs)

		country := c.GetHeader("CF-IPCountry")
		if country == "" {
			country = c.GetHeader("X-GeoIP-Country")
		}
		if country != "" {
			r.RecordCountryRequest(country, durMs)
		}
	}
}

func escapeLabelValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return v
}

// Render sérialise les métriques au format Prometheus Exposition 0.0.4.
func (r *Registry) Render(ctx context.Context) string {
	var b strings.Builder

	// --- Runtime & Process ---
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	uptime := time.Since(r.startTime).Seconds()

	b.WriteString("# HELP numan_process_uptime_seconds Process uptime in seconds.\n")
	b.WriteString("# TYPE numan_process_uptime_seconds gauge\n")
	fmt.Fprintf(&b, "numan_process_uptime_seconds %.3f\n", uptime)

	b.WriteString("# HELP numan_go_goroutines Number of active goroutines.\n")
	b.WriteString("# TYPE numan_go_goroutines gauge\n")
	fmt.Fprintf(&b, "numan_go_goroutines %d\n", runtime.NumGoroutine())

	b.WriteString("# HELP numan_go_mem_alloc_bytes Bytes of allocated heap objects.\n")
	b.WriteString("# TYPE numan_go_mem_alloc_bytes gauge\n")
	fmt.Fprintf(&b, "numan_go_mem_alloc_bytes %d\n", mem.Alloc)

	// --- HTTP ---
	b.WriteString("# HELP numan_http_requests_in_flight Current number of in-flight HTTP requests.\n")
	b.WriteString("# TYPE numan_http_requests_in_flight gauge\n")
	fmt.Fprintf(&b, "numan_http_requests_in_flight %d\n", r.httpInFlight.Load())

	r.httpMu.RLock()
	keys := make([]httpMetricKey, 0, len(r.httpCounters))
	vals := make(map[httpMetricKey]uint64, len(r.httpCounters))
	for k, c := range r.httpCounters {
		keys = append(keys, k)
		vals[k] = c.Load()
	}
	r.httpMu.RUnlock()

	sort.Slice(keys, func(i, j int) bool {
		if keys[i].path != keys[j].path {
			return keys[i].path < keys[j].path
		}
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		return keys[i].status < keys[j].status
	})

	b.WriteString("# HELP numan_http_requests_total Total number of HTTP requests processed.\n")
	b.WriteString("# TYPE numan_http_requests_total counter\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "numan_http_requests_total{method=\"%s\",path=\"%s\",status=\"%s\"} %d\n",
			escapeLabelValue(k.method), escapeLabelValue(k.path), escapeLabelValue(k.status), vals[k])
	}

	// --- Write-Behind Workers ---
	b.WriteString("# HELP numan_worker_batches_processed_total Total number of Write-Behind batches flushed.\n")
	b.WriteString("# TYPE numan_worker_batches_processed_total counter\n")
	fmt.Fprintf(&b, "numan_worker_batches_processed_total %d\n", r.workerBatches.Load())

	b.WriteString("# HELP numan_worker_events_processed_total Total number of Write-Behind events flushed.\n")
	b.WriteString("# TYPE numan_worker_events_processed_total counter\n")
	fmt.Fprintf(&b, "numan_worker_events_processed_total %d\n", r.workerEvents.Load())

	r.providerMu.RLock()
	wsFn := r.wsStatsFn
	pgFn := r.pgStatsFn
	pgReadFn := r.pgReadStatsFn
	qFn := r.queueDepthFn
	redisEvictedFn := r.redisEvictedFn
	r.providerMu.RUnlock()

	if qFn != nil {
		qCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		depth, ok := qFn(qCtx)
		cancel()
		if ok {
			b.WriteString("# HELP numan_worker_redis_queue_depth Total pending events across all Redis Write-Behind shards.\n")
			b.WriteString("# TYPE numan_worker_redis_queue_depth gauge\n")
			fmt.Fprintf(&b, "numan_worker_redis_queue_depth %d\n", depth)
		}
	}

	if redisEvictedFn != nil {
		rCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		evicted, ok := redisEvictedFn(rCtx)
		cancel()
		if ok {
			b.WriteString("# HELP numan_redis_evicted_keys_total Total number of evicted Redis keys (monitor volatile-lfu Tombstone pressure).\n")
			b.WriteString("# TYPE numan_redis_evicted_keys_total counter\n")
			fmt.Fprintf(&b, "numan_redis_evicted_keys_total %d\n", evicted)
		}
	}

	// --- WebSocket Hub ---
	if wsFn != nil {
		users, conns := wsFn()
		b.WriteString("# HELP numan_websocket_users_active Number of unique connected WebSocket users.\n")
		b.WriteString("# TYPE numan_websocket_users_active gauge\n")
		fmt.Fprintf(&b, "numan_websocket_users_active %d\n", users)

		b.WriteString("# HELP numan_websocket_connections_active Number of active WebSocket connections.\n")
		b.WriteString("# TYPE numan_websocket_connections_active gauge\n")
		fmt.Fprintf(&b, "numan_websocket_connections_active %d\n", conns)
	}

	// --- PostgreSQL Connection Pool (Primary / Write) ---
	if pgFn != nil {
		if stats, ok := pgFn(); ok {
			b.WriteString("# HELP numan_db_postgres_open_connections Number of established PostgreSQL Primary connections.\n")
			b.WriteString("# TYPE numan_db_postgres_open_connections gauge\n")
			fmt.Fprintf(&b, "numan_db_postgres_open_connections %d\n", stats.OpenConnections)

			b.WriteString("# HELP numan_db_postgres_in_use Number of PostgreSQL Primary connections currently in use.\n")
			b.WriteString("# TYPE numan_db_postgres_in_use gauge\n")
			fmt.Fprintf(&b, "numan_db_postgres_in_use %d\n", stats.InUse)

			b.WriteString("# HELP numan_db_postgres_idle Number of idle PostgreSQL Primary connections.\n")
			b.WriteString("# TYPE numan_db_postgres_idle gauge\n")
			fmt.Fprintf(&b, "numan_db_postgres_idle %d\n", stats.Idle)

			b.WriteString("# HELP numan_db_postgres_wait_count_total Total number of PostgreSQL Primary connections waited for.\n")
			b.WriteString("# TYPE numan_db_postgres_wait_count_total counter\n")
			fmt.Fprintf(&b, "numan_db_postgres_wait_count_total %d\n", stats.WaitCount)

			b.WriteString("# HELP numan_postgres_write_open_connections Number of established PostgreSQL Primary (Write) pool connections.\n")
			b.WriteString("# TYPE numan_postgres_write_open_connections gauge\n")
			fmt.Fprintf(&b, "numan_postgres_write_open_connections %d\n", stats.OpenConnections)
		}
	}

	// --- PostgreSQL Connection Pool (Read Replica) ---
	if pgReadFn != nil {
		if stats, ok := pgReadFn(); ok {
			b.WriteString("# HELP numan_postgres_read_open_connections Number of established PostgreSQL Read Replica pool connections.\n")
			b.WriteString("# TYPE numan_postgres_read_open_connections gauge\n")
			fmt.Fprintf(&b, "numan_postgres_read_open_connections %d\n", stats.OpenConnections)

			b.WriteString("# HELP numan_postgres_read_in_use Number of PostgreSQL Read Replica connections currently in use.\n")
			b.WriteString("# TYPE numan_postgres_read_in_use gauge\n")
			fmt.Fprintf(&b, "numan_postgres_read_in_use %d\n", stats.InUse)

			b.WriteString("# HELP numan_postgres_read_idle Number of idle PostgreSQL Read Replica connections.\n")
			b.WriteString("# TYPE numan_postgres_read_idle gauge\n")
			fmt.Fprintf(&b, "numan_postgres_read_idle %d\n", stats.Idle)

			b.WriteString("# HELP numan_postgres_read_wait_count_total Total number of PostgreSQL Read Replica connections waited for.\n")
			b.WriteString("# TYPE numan_postgres_read_wait_count_total counter\n")
			fmt.Fprintf(&b, "numan_postgres_read_wait_count_total %d\n", stats.WaitCount)
		}
	}

	r.renderExtendedMetrics(ctx, &b)
	r.renderOrphanCleanerMetrics(&b)

	return b.String()
}

// Handler expose /metrics au format Prometheus text/plain 0.0.4.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(r.Render(req.Context())))
	})
}

// Handler retourne le handler HTTP Prometheus du registre global.
func Handler() http.Handler {
	return defaultRegistry.Handler()
}
