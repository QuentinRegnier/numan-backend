package metrics

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/infra_telemetry_models"
)

const latencyRingCapacity = 1024

type extendedState struct {
	dcMu       sync.RWMutex
	datacenter string

	// HTTP RPS & p95 latency ring buffer
	httpSampleMu    sync.Mutex
	totalReqs       uint64
	lastDrainReqs   uint64
	lastDrainTime   time.Time
	latencyRing     [latencyRingCapacity]float64
	latencyRingIdx  int
	latencyRingFull bool

	// Privacy-safe GeoIP aggregates (ISO-2 country code only; ZERO IPs stored)
	geoMu         sync.Mutex
	geoTotalCount map[string]uint64
	geoTotalDurMs map[string]float64
	geoDeltaCount map[string]int64
	geoDeltaDurMs map[string]float64

	// Worker saturation tracking
	workerConcurrency atomic.Int64
	workerActive      atomic.Int64

	// Extended dynamic providers
	extProviderMu     sync.RWMutex
	queueOldestAgeFn  func(ctx context.Context) (float64, bool)
	dlqSizeFn         func(ctx context.Context) (int64, bool)
	redis3TierFn      func(ctx context.Context) []infra_telemetry_models.RedisTierMetricSnapshot
	schedulerMetricFn func() (infra_telemetry_models.SchedulerMetricSnapshot, bool)
	dcSaturationFn    func() (string, float64, bool)
	cuckooFillFn      func() (float64, bool)
}

func newExtendedState() *extendedState {
	return &extendedState{
		datacenter:    "default",
		lastDrainTime: time.Now(),
		geoTotalCount: make(map[string]uint64),
		geoTotalDurMs: make(map[string]float64),
		geoDeltaCount: make(map[string]int64),
		geoDeltaDurMs: make(map[string]float64),
	}
}

// SetDatacenter configure l'identifiant datacenter utilisé comme label Prometheus.
func (r *Registry) SetDatacenter(dc string) {
	dc = strings.ToLower(strings.TrimSpace(dc))
	if dc == "" {
		dc = "default"
	}
	r.ext.dcMu.Lock()
	r.ext.datacenter = dc
	r.ext.dcMu.Unlock()
}

// Datacenter retourne le datacenter configuré sur ce registre.
func (r *Registry) Datacenter() string {
	r.ext.dcMu.RLock()
	defer r.ext.dcMu.RUnlock()
	if r.ext.datacenter == "" {
		return "default"
	}
	return r.ext.datacenter
}

// RecordHTTPDuration enregistre la latence d'une requête HTTP dans le ring buffer p95.
func (r *Registry) RecordHTTPDuration(durationMs float64) {
	if durationMs < 0 || math.IsNaN(durationMs) || math.IsInf(durationMs, 0) {
		return
	}
	r.ext.httpSampleMu.Lock()
	r.ext.totalReqs++
	r.ext.latencyRing[r.ext.latencyRingIdx] = durationMs
	r.ext.latencyRingIdx = (r.ext.latencyRingIdx + 1) % latencyRingCapacity
	if r.ext.latencyRingIdx == 0 {
		r.ext.latencyRingFull = true
	}
	r.ext.httpSampleMu.Unlock()
}

// RecordCountryRequest incrémente les compteurs GeoIP privacy-safe (strictement code pays ISO-2, zéro IP).
func (r *Registry) RecordCountryRequest(rawCountry string, durationMs float64) {
	cc := infra_telemetry_models.NormalizeCountryCode(rawCountry)
	if durationMs < 0 || math.IsNaN(durationMs) || math.IsInf(durationMs, 0) {
		durationMs = 0
	}
	r.ext.geoMu.Lock()
	r.ext.geoTotalCount[cc]++
	r.ext.geoTotalDurMs[cc] += durationMs
	r.ext.geoDeltaCount[cc]++
	r.ext.geoDeltaDurMs[cc] += durationMs
	r.ext.geoMu.Unlock()
}

// DrainGeoIPDelta retourne et remet à zéro le delta GeoIP depuis le dernier heartbeat.
func (r *Registry) DrainGeoIPDelta() map[string]infra_telemetry_models.CountryDelta {
	r.ext.geoMu.Lock()
	defer r.ext.geoMu.Unlock()
	if len(r.ext.geoDeltaCount) == 0 {
		return nil
	}
	out := make(map[string]infra_telemetry_models.CountryDelta, len(r.ext.geoDeltaCount))
	for cc, cnt := range r.ext.geoDeltaCount {
		out[cc] = infra_telemetry_models.CountryDelta{
			Requests:      cnt,
			DurationMsSum: r.ext.geoDeltaDurMs[cc],
		}
	}
	r.ext.geoDeltaCount = make(map[string]int64)
	r.ext.geoDeltaDurMs = make(map[string]float64)
	return out
}

// SnapshotHTTPStats calcule le RPS depuis le dernier appel, les requêtes in-flight et la latence p95 (ms).
func (r *Registry) SnapshotHTTPStats() (rps float64, inFlight int64, p95Ms float64) {
	inFlight = r.httpInFlight.Load()

	r.ext.httpSampleMu.Lock()
	now := time.Now()
	elapsed := now.Sub(r.ext.lastDrainTime).Seconds()
	deltaReqs := r.ext.totalReqs - r.ext.lastDrainReqs
	if elapsed > 0.05 {
		rps = float64(deltaReqs) / elapsed
		r.ext.lastDrainReqs = r.ext.totalReqs
		r.ext.lastDrainTime = now
	}

	n := r.ext.latencyRingIdx
	if r.ext.latencyRingFull {
		n = latencyRingCapacity
	}
	var samples []float64
	if n > 0 {
		samples = make([]float64, n)
		copy(samples, r.ext.latencyRing[:n])
	}
	r.ext.httpSampleMu.Unlock()

	if len(samples) > 0 {
		sort.Float64s(samples)
		idx := int(math.Ceil(0.95*float64(len(samples)))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(samples) {
			idx = len(samples) - 1
		}
		p95Ms = samples[idx]
	}
	return rps, inFlight, p95Ms
}

// SetWorkerConcurrency configure la concurrence Write-Behind observée.
func (r *Registry) SetWorkerConcurrency(n int) {
	if n < 0 {
		n = 0
	}
	r.ext.workerConcurrency.Store(int64(n))
}

// IncWorkerActive / DecWorkerActive suivent les goroutines Write-Behind en cours de flush.
func (r *Registry) IncWorkerActive() { r.ext.workerActive.Add(1) }
func (r *Registry) DecWorkerActive() { r.ext.workerActive.Add(-1) }

// WorkerSaturation retourne la concurrence configurée, les goroutines actives et le ratio de saturation [0..1].
func (r *Registry) WorkerSaturation() (configured int64, active int64, ratio float64) {
	configured = r.ext.workerConcurrency.Load()
	active = r.ext.workerActive.Load()
	if active < 0 {
		active = 0
	}
	if configured > 0 {
		ratio = float64(active) / float64(configured)
		if ratio > 1 {
			ratio = 1
		}
	}
	return configured, active, ratio
}

// SetWorkerConcurrency configure la concurrence sur le registre global.
func SetWorkerConcurrency(n int) { defaultRegistry.SetWorkerConcurrency(n) }

// IncWorkerActive / DecWorkerActive suivent l'activité Write-Behind sur le registre global.
func IncWorkerActive() { defaultRegistry.IncWorkerActive() }
func DecWorkerActive() { defaultRegistry.DecWorkerActive() }

// SetQueueOldestAgeProvider enregistre la sonde d'âge du plus ancien événement en attente.
func (r *Registry) SetQueueOldestAgeProvider(fn func(ctx context.Context) (float64, bool)) {
	r.ext.extProviderMu.Lock()
	r.ext.queueOldestAgeFn = fn
	r.ext.extProviderMu.Unlock()
}

// SetDLQSizeProvider enregistre la sonde de taille de la Dead Letter Queue.
func (r *Registry) SetDLQSizeProvider(fn func(ctx context.Context) (int64, bool)) {
	r.ext.extProviderMu.Lock()
	r.ext.dlqSizeFn = fn
	r.ext.extProviderMu.Unlock()
}

// SetRedis3TierProvider enregistre la sonde télémétrie des 3 tiers Redis.
func (r *Registry) SetRedis3TierProvider(fn func(ctx context.Context) []infra_telemetry_models.RedisTierMetricSnapshot) {
	r.ext.extProviderMu.Lock()
	r.ext.redis3TierFn = fn
	r.ext.extProviderMu.Unlock()
}

// SetSchedulerMetricsProvider enregistre la sonde d'état du Scheduler Activity-Aware.
func (r *Registry) SetSchedulerMetricsProvider(fn func() (infra_telemetry_models.SchedulerMetricSnapshot, bool)) {
	r.ext.extProviderMu.Lock()
	r.ext.schedulerMetricFn = fn
	r.ext.extProviderMu.Unlock()
}

// SetDCSaturationProvider enregistre la sonde de saturation globale du datacenter.
func (r *Registry) SetDCSaturationProvider(fn func() (string, float64, bool)) {
	r.ext.extProviderMu.Lock()
	r.ext.dcSaturationFn = fn
	r.ext.extProviderMu.Unlock()
}

// SetCuckooFillProvider enregistre la sonde de taux de remplissage du Cuckoo Filter.
func (r *Registry) SetCuckooFillProvider(fn func() (float64, bool)) {
	r.ext.extProviderMu.Lock()
	r.ext.cuckooFillFn = fn
	r.ext.extProviderMu.Unlock()
}

func (r *Registry) renderExtendedMetrics(ctx context.Context, b *strings.Builder) {
	dc := escapeLabelValue(r.Datacenter())

	// --- Privacy-safe GeoIP HTTP counters (ISO-2 country codes only, zero raw IPs) ---
	r.ext.geoMu.Lock()
	countries := make([]string, 0, len(r.ext.geoTotalCount))
	geoCounts := make(map[string]uint64, len(r.ext.geoTotalCount))
	geoDurSums := make(map[string]float64, len(r.ext.geoTotalDurMs))
	for cc, cnt := range r.ext.geoTotalCount {
		countries = append(countries, cc)
		geoCounts[cc] = cnt
		geoDurSums[cc] = r.ext.geoTotalDurMs[cc]
	}
	r.ext.geoMu.Unlock()

	if len(countries) > 0 {
		sort.Strings(countries)
		b.WriteString("# HELP numan_http_requests_by_country_total Privacy-safe HTTP requests aggregated by ISO-2 country code.\n")
		b.WriteString("# TYPE numan_http_requests_by_country_total counter\n")
		for _, cc := range countries {
			fmt.Fprintf(b, "numan_http_requests_by_country_total{datacenter=\"%s\",country=\"%s\"} %d\n",
				dc, escapeLabelValue(cc), geoCounts[cc])
		}
		b.WriteString("# HELP numan_http_request_duration_ms_sum_by_country Privacy-safe sum of HTTP request durations in milliseconds by country.\n")
		b.WriteString("# TYPE numan_http_request_duration_ms_sum_by_country counter\n")
		for _, cc := range countries {
			fmt.Fprintf(b, "numan_http_request_duration_ms_sum_by_country{datacenter=\"%s\",country=\"%s\"} %.3f\n",
				dc, escapeLabelValue(cc), geoDurSums[cc])
		}
	}

	// --- Worker concurrency & saturation ---
	configured, _, satRatio := r.WorkerSaturation()
	if configured > 0 {
		b.WriteString("# HELP numan_worker_concurrency_configured Configured Write-Behind worker concurrency.\n")
		b.WriteString("# TYPE numan_worker_concurrency_configured gauge\n")
		fmt.Fprintf(b, "numan_worker_concurrency_configured %d\n", configured)

		b.WriteString("# HELP numan_worker_saturation_ratio Fraction of Write-Behind workers actively flushing batches (0..1).\n")
		b.WriteString("# TYPE numan_worker_saturation_ratio gauge\n")
		fmt.Fprintf(b, "numan_worker_saturation_ratio %.4f\n", satRatio)
	}

	r.providerMu.RLock()
	qFn := r.queueDepthFn
	r.providerMu.RUnlock()

	r.ext.extProviderMu.RLock()
	oldestAgeFn := r.ext.queueOldestAgeFn
	dlqFn := r.ext.dlqSizeFn
	redis3Fn := r.ext.redis3TierFn
	schedFn := r.ext.schedulerMetricFn
	dcSatFn := r.ext.dcSaturationFn
	cuckooFn := r.ext.cuckooFillFn
	r.ext.extProviderMu.RUnlock()

	if qFn != nil {
		qCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		depth, ok := qFn(qCtx)
		cancel()
		if ok {
			b.WriteString("# HELP numan_worker_queue_depth_total Total pending Write-Behind events across all 64 shards.\n")
			b.WriteString("# TYPE numan_worker_queue_depth_total gauge\n")
			fmt.Fprintf(b, "numan_worker_queue_depth_total %d\n", depth)
		}
	}

	if oldestAgeFn != nil {
		oCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		ageSec, ok := oldestAgeFn(oCtx)
		cancel()
		if ok {
			b.WriteString("# HELP numan_worker_queue_oldest_event_age_seconds Age in seconds of the oldest pending Write-Behind event.\n")
			b.WriteString("# TYPE numan_worker_queue_oldest_event_age_seconds gauge\n")
			fmt.Fprintf(b, "numan_worker_queue_oldest_event_age_seconds %.3f\n", ageSec)
		}
	}

	if dlqFn != nil {
		dCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		dlqSize, ok := dlqFn(dCtx)
		cancel()
		if ok {
			b.WriteString("# HELP numan_dlq_size Current number of events in the Write-Behind Dead Letter Queue.\n")
			b.WriteString("# TYPE numan_dlq_size gauge\n")
			fmt.Fprintf(b, "numan_dlq_size %d\n", dlqSize)
		}
	}

	// --- 3-Tier Redis Telemetry ---
	if redis3Fn != nil {
		rCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		tiers := redis3Fn(rCtx)
		cancel()
		if len(tiers) > 0 {
			b.WriteString("# HELP numan_redis_tier_mode Active topology mode per Redis tier (1 = active).\n")
			b.WriteString("# TYPE numan_redis_tier_mode gauge\n")
			for _, t := range tiers {
				fmt.Fprintf(b, "numan_redis_tier_mode{tier=\"%s\",mode=\"%s\"} 1\n",
					escapeLabelValue(t.Tier), escapeLabelValue(t.Mode))
			}

			b.WriteString("# HELP numan_redis_tier_evicted_keys_total Evicted keys counter per Redis tier.\n")
			b.WriteString("# TYPE numan_redis_tier_evicted_keys_total counter\n")
			for _, t := range tiers {
				fmt.Fprintf(b, "numan_redis_tier_evicted_keys_total{tier=\"%s\"} %d\n",
					escapeLabelValue(t.Tier), t.EvictedKeys)
			}

			hasShards := false
			for _, t := range tiers {
				if len(t.Shards) > 0 {
					hasShards = true
					break
				}
			}
			if hasShards {
				b.WriteString("# HELP numan_redis_shard_used_memory_bytes Used memory in bytes per Redis tier shard.\n")
				b.WriteString("# TYPE numan_redis_shard_used_memory_bytes gauge\n")
				for _, t := range tiers {
					for _, s := range t.Shards {
						fmt.Fprintf(b, "numan_redis_shard_used_memory_bytes{tier=\"%s\",shard=\"%s\",role=\"%s\",node=\"%s\"} %d\n",
							escapeLabelValue(t.Tier), escapeLabelValue(s.ShardID), escapeLabelValue(s.Role), escapeLabelValue(s.Node), s.UsedMemoryBytes)
					}
				}
				b.WriteString("# HELP numan_redis_shard_max_memory_bytes Max memory limit in bytes per Redis tier shard.\n")
				b.WriteString("# TYPE numan_redis_shard_max_memory_bytes gauge\n")
				for _, t := range tiers {
					for _, s := range t.Shards {
						fmt.Fprintf(b, "numan_redis_shard_max_memory_bytes{tier=\"%s\",shard=\"%s\",role=\"%s\",node=\"%s\"} %d\n",
							escapeLabelValue(t.Tier), escapeLabelValue(s.ShardID), escapeLabelValue(s.Role), escapeLabelValue(s.Node), s.MaxMemoryBytes)
					}
				}
				b.WriteString("# HELP numan_redis_shard_fill_ratio Memory fill ratio (0..1) per Redis tier shard.\n")
				b.WriteString("# TYPE numan_redis_shard_fill_ratio gauge\n")
				for _, t := range tiers {
					for _, s := range t.Shards {
						fmt.Fprintf(b, "numan_redis_shard_fill_ratio{tier=\"%s\",shard=\"%s\",role=\"%s\",node=\"%s\"} %.4f\n",
							escapeLabelValue(t.Tier), escapeLabelValue(s.ShardID), escapeLabelValue(s.Role), escapeLabelValue(s.Node), s.FillRatio)
					}
				}
			}

			for _, t := range tiers {
				if t.Tier == "pubsub" {
					b.WriteString("# HELP numan_redis_pubsub_active_channels Active Pub/Sub channels on Tier 3 Redis.\n")
					b.WriteString("# TYPE numan_redis_pubsub_active_channels gauge\n")
					fmt.Fprintf(b, "numan_redis_pubsub_active_channels %d\n", t.PubSubChannels)

					b.WriteString("# HELP numan_redis_pubsub_messages_per_sec Estimated Pub/Sub messages per second.\n")
					b.WriteString("# TYPE numan_redis_pubsub_messages_per_sec gauge\n")
					fmt.Fprintf(b, "numan_redis_pubsub_messages_per_sec %.2f\n", t.PubSubMsgPerSec)
				}
			}
		}
	}

	// --- Activity-Aware Job Scheduler ---
	if schedFn != nil {
		if snap, ok := schedFn(); ok {
			sdc := escapeLabelValue(snap.Datacenter)
			if sdc == "" {
				sdc = dc
			}
			b.WriteString("# HELP numan_scheduler_regime Current Activity-Aware Scheduler regime (1 = active).\n")
			b.WriteString("# TYPE numan_scheduler_regime gauge\n")
			for _, reg := range []string{"OVERLOAD", "PEAK", "MODERATE", "IDLE", "DEEP_IDLE"} {
				val := 0
				if snap.Regime == reg {
					val = 1
				}
				fmt.Fprintf(b, "numan_scheduler_regime{datacenter=\"%s\",regime=\"%s\"} %d\n", sdc, reg, val)
			}

			starvVal := 0
			if snap.StarvationAlert {
				starvVal = 1
			}
			b.WriteString("# HELP numan_scheduler_starvation_alert 1 if no DEEP_IDLE window occurred for >= 48h.\n")
			b.WriteString("# TYPE numan_scheduler_starvation_alert gauge\n")
			fmt.Fprintf(b, "numan_scheduler_starvation_alert{datacenter=\"%s\"} %d\n", sdc, starvVal)

			if len(snap.Jobs) > 0 {
				b.WriteString("# HELP numan_scheduler_job_running 1 if the scheduled job is currently running.\n")
				b.WriteString("# TYPE numan_scheduler_job_running gauge\n")
				for _, j := range snap.Jobs {
					runVal := 0
					if j.Running {
						runVal = 1
					}
					fmt.Fprintf(b, "numan_scheduler_job_running{job=\"%s\"} %d\n", escapeLabelValue(j.Name), runVal)
				}

				b.WriteString("# HELP numan_scheduler_job_duration_seconds Last execution duration in seconds of each scheduled job.\n")
				b.WriteString("# TYPE numan_scheduler_job_duration_seconds gauge\n")
				for _, j := range snap.Jobs {
					fmt.Fprintf(b, "numan_scheduler_job_duration_seconds{job=\"%s\"} %.3f\n", escapeLabelValue(j.Name), j.LastDurationSec)
				}
			}
		}
	}

	// --- Datacenter Saturation & Cuckoo Filter ---
	if dcSatFn != nil {
		if satDC, sat, ok := dcSatFn(); ok {
			if satDC == "" {
				satDC = dc
			}
			b.WriteString("# HELP numan_dc_saturation_ratio Composite datacenter saturation ratio (0..1).\n")
			b.WriteString("# TYPE numan_dc_saturation_ratio gauge\n")
			fmt.Fprintf(b, "numan_dc_saturation_ratio{datacenter=\"%s\"} %.4f\n", escapeLabelValue(satDC), sat)
		}
	}

	if cuckooFn != nil {
		if fill, ok := cuckooFn(); ok {
			b.WriteString("# HELP numan_cuckoo_fill_ratio Cuckoo filter fill ratio (0..1).\n")
			b.WriteString("# TYPE numan_cuckoo_fill_ratio gauge\n")
			fmt.Fprintf(b, "numan_cuckoo_fill_ratio %.4f\n", fill)
		}
	}
}
