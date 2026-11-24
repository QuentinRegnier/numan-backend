package infra_telemetry_service

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/infra_telemetry_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	repoPostgres "github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
)

const (
	defaultAggInterval  = 10 * time.Second
	liveSnapshotTTL     = 60 * time.Second
	hourlyProfileTTL    = 14 * 24 * time.Hour
	historyMaxSamples1m = 1440 // 24h de points 1-minute
	geoIPAggregateTTL   = 24 * time.Hour
	defaultMaxRPSRef    = 2000.0
)

// AggregatorConfig configure l'agrégateur télémétrie datacenter (exécuté sur scheduler_node / monolith).
type AggregatorConfig struct {
	DatacenterID string
	Interval     time.Duration
	Location     *time.Location
	Scheduler    *scheduler_service.Engine
	MaxRPSRef    float64
	NowFn        func() time.Time
}

var (
	latestSnapMu sync.RWMutex
	latestSnap   *infra_telemetry_models.CLITelemetrySnapshot
)

// LatestCachedSnapshot retourne le dernier snapshot DC en RAM (pour exposition Prometheus / fallback rapide).
func LatestCachedSnapshot() *infra_telemetry_models.CLITelemetrySnapshot {
	latestSnapMu.RLock()
	defer latestSnapMu.RUnlock()
	if latestSnap == nil {
		return nil
	}
	cp := *latestSnap
	return &cp
}

func setLatestCachedSnapshot(s *infra_telemetry_models.CLITelemetrySnapshot) {
	if s == nil {
		return
	}
	cp := *s
	latestSnapMu.Lock()
	latestSnap = &cp
	latestSnapMu.Unlock()
}

// GetLiveTelemetrySnapshot lit le snapshot unifié depuis Redis Core (`infra:telemetry:dc:<dc_id>:live`) en <1ms via cache_service.
func GetLiveTelemetrySnapshot(ctx context.Context, datacenter string) (*infra_telemetry_models.CLITelemetrySnapshot, error) {
	if strings.TrimSpace(datacenter) == "" {
		datacenter = "default"
	}
	if snap, err := cache_service.GetLiveTelemetrySnapshot(ctx, datacenter); err == nil && snap != nil {
		return snap, nil
	}
	if cached := LatestCachedSnapshot(); cached != nil && (datacenter == "" || cached.Datacenter == datacenter) {
		return cached, nil
	}
	return nil, nil
}

// GetOrComputeTelemetrySnapshot retourne le snapshot temps réel depuis Redis Core (<1ms),
// ou déclenche une agrégation instantanée de secours si aucune entrée n'est encore présente.
func GetOrComputeTelemetrySnapshot(ctx context.Context, datacenter string) (*infra_telemetry_models.CLITelemetrySnapshot, error) {
	datacenter = strings.TrimSpace(datacenter)
	if datacenter == "" {
		datacenter = strings.TrimSpace(os.Getenv("DATACENTER_ID"))
	}
	if datacenter == "" {
		datacenter = "default"
	}
	if snap, err := GetLiveTelemetrySnapshot(ctx, datacenter); err == nil && snap != nil {
		return snap, nil
	}
	return RunTelemetryAggregationOnce(ctx, AggregatorConfig{
		DatacenterID: datacenter,
		Scheduler:    scheduler_service.Default(),
	})
}

// StartDatacenterTelemetryAggregator lance l'agrégateur toutes les 10s (protégé par verrou distribué via cache_service).
func StartDatacenterTelemetryAggregator(parentCtx context.Context, cfg AggregatorConfig) func() {
	if cfg.DatacenterID == "" {
		cfg.DatacenterID = "default"
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultAggInterval
	}
	if cfg.Location == nil {
		cfg.Location = time.Local
	}
	if cfg.NowFn == nil {
		cfg.NowFn = time.Now
	}

	ctx, cancel := context.WithCancel(parentCtx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		_, _ = RunTelemetryAggregationOnce(ctx, cfg)
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				lockToken, acquired, errLock := cache_service.AcquireTelemetryAggregatorLock(ctx, 15*time.Second)
				if errLock == nil && !acquired {
					continue
				}
				if _, err := RunTelemetryAggregationOnce(ctx, cfg); err != nil {
					numan_log.Warn(ctx).Err(err).Str("datacenter", cfg.DatacenterID).Msg("Échec agrégation télémétrie DC")
				}
				if lockToken != "" {
					cache_service.ReleaseTelemetryAggregatorLock(context.Background(), lockToken)
				}
			}
		}
	}()

	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}

// RunTelemetryAggregationOnce agrège les heartbeats pods + 3 tiers Redis + Postgres/Mongo + Scheduler + GeoIP,
// met à jour le régime du Scheduler et persiste `live`, `hourly_profile` et `history:1m` dans Redis Core.
func RunTelemetryAggregationOnce(ctx context.Context, cfg AggregatorConfig) (*infra_telemetry_models.CLITelemetrySnapshot, error) {
	if cfg.DatacenterID == "" {
		cfg.DatacenterID = "default"
	}
	if cfg.Location == nil {
		cfg.Location = time.Local
	}
	if cfg.NowFn == nil {
		cfg.NowFn = time.Now
	}
	if cfg.MaxRPSRef <= 0 {
		cfg.MaxRPSRef = defaultMaxRPSRef
	}

	now := cfg.NowFn()
	nowMs := domain.TimeToMillis(now)

	// 1. Scanner tous les heartbeats actifs via cache_service
	pods, _ := cache_service.ListActivePodHeartbeats(ctx, cfg.DatacenterID)

	// 2. Agréger les métriques par serveur physique et au niveau DC
	type serverAccum struct {
		hostname   string
		roleSet    map[string]struct{}
		cpuMax     float64
		ramBytes   uint64
		rpsSum     float64
		p95Max     float64
		wsConns    int64
		wsCapMax   float64
		workerConc int64
		workerSat  float64
		mqDepth    int64
		mqLagSec   float64
		dlqSize    int64
		cuckooFill float64
		pgWriteMax float64
		pgReadMax  float64
	}

	byServer := make(map[string]*serverAccum)
	var (
		totalRPS      float64
		maxP95        float64
		maxWSCap      float64
		maxWorkerSat  float64
		maxQueueAge   float64
		maxPGWrite    float64
		maxPGRead     float64
		maxCPURatio   float64
		geoDeltaBatch = make(map[string]infra_telemetry_models.CountryDelta)
	)

	for _, p := range pods {
		host := p.Hostname
		if host == "" {
			host = p.PodName
		}
		acc, ok := byServer[host]
		if !ok {
			acc = &serverAccum{hostname: host, roleSet: make(map[string]struct{})}
			byServer[host] = acc
		}
		if p.Role != "" {
			acc.roleSet[p.Role] = struct{}{}
		}
		if p.GoCPUUsageRatio > acc.cpuMax {
			acc.cpuMax = p.GoCPUUsageRatio
		}
		acc.ramBytes += p.GoMemoryAllocBytes
		acc.rpsSum += p.HTTPRequestsPerSec
		if p.HTTPP95LatencyMs > acc.p95Max {
			acc.p95Max = p.HTTPP95LatencyMs
		}
		acc.wsConns += p.WSActiveConnections
		if p.WSCapacityRatio > acc.wsCapMax {
			acc.wsCapMax = p.WSCapacityRatio
		}
		acc.workerConc += p.WorkerConcurrency
		if p.WorkerSaturationRatio > acc.workerSat {
			acc.workerSat = p.WorkerSaturationRatio
		}
		if p.QueueDepthTotal > acc.mqDepth {
			acc.mqDepth = p.QueueDepthTotal
		}
		if p.QueueOldestEventAgeSec > acc.mqLagSec {
			acc.mqLagSec = p.QueueOldestEventAgeSec
		}
		if p.DLQSize > acc.dlqSize {
			acc.dlqSize = p.DLQSize
		}
		if p.CuckooFillRatio > acc.cuckooFill {
			acc.cuckooFill = p.CuckooFillRatio
		}
		if p.PostgresWritePoolUtil > acc.pgWriteMax {
			acc.pgWriteMax = p.PostgresWritePoolUtil
		}
		if p.PostgresReadPoolUtil > acc.pgReadMax {
			acc.pgReadMax = p.PostgresReadPoolUtil
		}

		totalRPS += p.HTTPRequestsPerSec
		if p.HTTPP95LatencyMs > maxP95 {
			maxP95 = p.HTTPP95LatencyMs
		}
		if p.WSCapacityRatio > maxWSCap {
			maxWSCap = p.WSCapacityRatio
		}
		if p.WorkerSaturationRatio > maxWorkerSat {
			maxWorkerSat = p.WorkerSaturationRatio
		}
		if p.QueueOldestEventAgeSec > maxQueueAge {
			maxQueueAge = p.QueueOldestEventAgeSec
		}
		if p.PostgresWritePoolUtil > maxPGWrite {
			maxPGWrite = p.PostgresWritePoolUtil
		}
		if p.PostgresReadPoolUtil > maxPGRead {
			maxPGRead = p.PostgresReadPoolUtil
		}
		if p.GoCPUUsageRatio > maxCPURatio {
			maxCPURatio = p.GoCPUUsageRatio
		}

		for cc, d := range p.GeoIPCountryCountsDelta {
			norm := infra_telemetry_models.NormalizeCountryCode(cc)
			cur := geoDeltaBatch[norm]
			cur.Requests += d.Requests
			cur.DurationMsSum += d.DurationMsSum
			geoDeltaBatch[norm] = cur
		}
	}

	servers := make([]infra_telemetry_models.ServerStressSnapshot, 0, len(byServer))
	for _, acc := range byServer {
		roles := make([]string, 0, len(acc.roleSet))
		for r := range acc.roleSet {
			roles = append(roles, r)
		}
		sort.Strings(roles)

		hint := deriveBottleneckHint(acc.cpuMax, acc.p95Max, acc.workerSat, acc.mqDepth, acc.mqLagSec, acc.wsCapMax, acc.pgWriteMax, acc.cuckooFill)
		ramRatio := clamp01(float64(acc.ramBytes) / float64(4*1024*1024*1024))

		servers = append(servers, infra_telemetry_models.ServerStressSnapshot{
			Hostname:          acc.hostname,
			Roles:             roles,
			CPURatio:          round4(acc.cpuMax),
			RAMRatio:          round4(ramRatio),
			RPS:               round2(acc.rpsSum),
			P95Ms:             round2(acc.p95Max),
			WSConns:           acc.wsConns,
			WorkerConcurrency: acc.workerConc,
			WorkerSaturation:  round4(acc.workerSat),
			MQDepth:           acc.mqDepth,
			MQLagSec:          round2(acc.mqLagSec),
			DLQSize:           acc.dlqSize,
			CuckooFillRatio:   round4(acc.cuckooFill),
			BottleneckHint:    hint,
		})
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].Hostname < servers[j].Hostname })

	// 3. Collecter la télémétrie 3-Tier Redis via cache_service
	redisTiers := cache_service.CollectRedis3TierSnapshot(ctx)

	// 4. Calculer le score composite DC Activity (0..1) et DC Saturation (0..1)
	rpsScore := clamp01(totalRPS / cfg.MaxRPSRef)
	activityScore := clamp01(0.45*rpsScore + 0.25*maxWSCap + 0.20*maxWorkerSat + 0.10*maxCPURatio)
	saturationScore := maxFloat(maxCPURatio, maxWorkerSat, maxPGWrite, maxWSCap, clamp01(maxP95/1000.0))

	// 5. Mettre à jour le profil horaire glissant 7j et détecter l'alerte saturation hors-pic
	localHour := now.In(cfg.Location).Hour()
	hourlyProfile := updateHourlyProfile(ctx, cfg.DatacenterID, localHour, activityScore, nowMs)
	offPeakWarning := detectOffPeakSaturation(hourlyProfile, localHour, saturationScore)

	// 6. Évaluer le régime du Scheduler (préemption instantanée si PEAK/OVERLOAD!)
	sample := scheduler_models.ActivitySample{
		ActivityScore:          activityScore,
		SaturationScore:        saturationScore,
		HTTPP95LatencyMs:       maxP95,
		QueueOldestEventAgeSec: maxQueueAge,
		PostgresWritePoolUtil:  maxPGWrite,
	}

	var (
		regime          = scheduler_service.ClassifyRawRegime(scheduler_service.DefaultRegimeConfig(), sample)
		schedSnap       = infra_telemetry_models.SchedulerSnapshot{}
		starvationAlert = false
	)
	if cfg.Scheduler != nil {
		regime = cfg.Scheduler.EvaluateSample(ctx, sample)
		ov := cfg.Scheduler.StatusSnapshot(ctx)
		starvationAlert = ov.Starvation48hAlert
		schedSnap = infra_telemetry_models.SchedulerSnapshot{
			Paused:       ov.Paused,
			PauseReason:  ov.PauseReason,
			ActiveJob:    ov.ActiveJob,
			ProgressPct:  round2(ov.ProgressPct),
			CPUBudgetPct: ov.CPUBudgetPct,
			JobsSummary:  ov.Jobs,
		}
	}

	// 7. Mettre à jour l'agrégat privacy-safe GeoIP 24h (strictement ISO-2)
	geoStats := updateGeoIP24h(ctx, cfg.DatacenterID, nowMs, geoDeltaBatch)

	// 8. Mettre à jour le ring d'historique 1-minute (1440 points = 24h)
	historySample := infra_telemetry_models.HistorySample1m{
		TimestampMs: nowMs,
		Activity:    round4(activityScore),
		Saturation:  round4(saturationScore),
		RPS:         round2(totalRPS),
		Regime:      string(regime),
	}
	recentHistory := cache_service.AppendTelemetryHistory1m(ctx, cfg.DatacenterID, historySample, historyMaxSamples1m, 60)

	// 9. Assembler le snapshot unifié
	snap := &infra_telemetry_models.CLITelemetrySnapshot{
		Datacenter:               cfg.DatacenterID,
		TimestampMs:              nowMs,
		DCSaturationRatio:        round4(saturationScore),
		DCActivityScore:          round4(activityScore),
		SchedulerRegime:          regime,
		OffPeakSaturationWarning: offPeakWarning,
		Starvation48hWarning:     starvationAlert,
		HourlyBaseline24h:        hourlyProfile.Hours,
		Scheduler:                schedSnap,
		RedisTiers:               redisTiers,
		Databases: infra_telemetry_models.DatabasesSnapshot{
			Postgres: infra_telemetry_models.PostgresTopologySnapshot{
				Primary:       envOrDefault("POSTGRES_HOST", "postgres-primary"),
				Replicas:      splitCSVEnv("POSTGRES_READ_HOST"),
				PoolWriteUtil: round4(maxPGWrite),
				PoolReadUtil:  round4(maxPGRead),
			},
			Mongo: infra_telemetry_models.MongoTopologySnapshot{
				Primary:     envOrDefault("MONGO_HOST", "mongo-0"),
				Secondaries: splitCSVEnv("MONGO_SECONDARIES"),
				LagSec:      0,
			},
		},
		Servers:   servers,
		GeoIP24h:  geoStats,
		History1m: recentHistory,
	}

	if snap.Databases.Postgres.PoolWriteUtil == 0 {
		if st, ok := repoPostgres.FuncWritePoolStats(); ok {
			maxOpen := st.MaxOpenConnections
			if maxOpen <= 0 {
				maxOpen = 25
			}
			snap.Databases.Postgres.PoolWriteUtil = round4(clamp01(float64(st.InUse) / float64(maxOpen)))
		}
	}

	setLatestCachedSnapshot(snap)
	_ = cache_service.SaveLiveTelemetrySnapshot(ctx, cfg.DatacenterID, snap, liveSnapshotTTL)
	return snap, nil
}

// CollectRedis3TierMetrics adapte la télémétrie 3-Tier Redis au format Prometheus `infra_telemetry_models.RedisTierMetricSnapshot`.
func CollectRedis3TierMetrics(ctx context.Context) []infra_telemetry_models.RedisTierMetricSnapshot {
	tiers := cache_service.CollectRedis3TierSnapshot(ctx)
	build := func(name string, t infra_telemetry_models.RedisTierSnapshot) infra_telemetry_models.RedisTierMetricSnapshot {
		ms := infra_telemetry_models.RedisTierMetricSnapshot{
			Tier:            name,
			Mode:            t.Mode,
			EvictedKeys:     t.EvictedKeys,
			PubSubChannels:  t.Channels,
			PubSubMsgPerSec: t.MsgPerSec,
			Shards:          make([]infra_telemetry_models.RedisShardMetric, 0, len(t.ShardDetails)),
		}
		for _, s := range t.ShardDetails {
			ms.Shards = append(ms.Shards, infra_telemetry_models.RedisShardMetric{
				Tier:             name,
				ShardID:          s.ShardID,
				Role:             s.Role,
				Node:             s.Node,
				UsedMemoryBytes:  s.UsedMemoryBytes,
				MaxMemoryBytes:   s.MaxMemoryBytes,
				FillRatio:        s.FillRatio,
				ConnectedClients: s.ConnectedClients,
			})
		}
		return ms
	}
	return []infra_telemetry_models.RedisTierMetricSnapshot{
		build("core", tiers.Core),
		build("objects", tiers.ObjectPool),
		build("pubsub", tiers.PubSub),
	}
}

func updateHourlyProfile(ctx context.Context, dc string, hour int, activity float64, nowMs int64) infra_telemetry_models.HourlyProfile {
	prof := cache_service.GetHourlyProfile(ctx, dc)
	if hour < 0 || hour > 23 {
		return prof
	}
	if prof.SampleCount[hour] == 0 {
		prof.Hours[hour] = round4(activity)
	} else {
		alpha := 0.05
		prof.Hours[hour] = round4((1-alpha)*prof.Hours[hour] + alpha*activity)
	}
	prof.SampleCount[hour]++
	prof.UpdatedAtMs = nowMs
	cache_service.SaveHourlyProfile(ctx, dc, prof, hourlyProfileTTL)
	return prof
}

func detectOffPeakSaturation(prof infra_telemetry_models.HourlyProfile, currentHour int, saturation float64) bool {
	if saturation < 0.75 || currentHour < 0 || currentHour > 23 {
		return false
	}
	var peakHourVal float64
	for _, v := range prof.Hours {
		if v > peakHourVal {
			peakHourVal = v
		}
	}
	if peakHourVal <= 0.05 {
		return false
	}
	return prof.Hours[currentHour] < peakHourVal*0.80
}

func updateGeoIP24h(ctx context.Context, dc string, nowMs int64, delta map[string]infra_telemetry_models.CountryDelta) infra_telemetry_models.GeoIP24hSnapshot {
	bucket := cache_service.GetGeoIPBucket(ctx, dc)
	if bucket.UpdatedAtMs > 0 && nowMs-bucket.UpdatedAtMs > int64((24*time.Hour)/time.Millisecond) {
		bucket.Countries = make(map[string]infra_telemetry_models.CountryDelta)
	}
	for cc, d := range delta {
		norm := infra_telemetry_models.NormalizeCountryCode(cc)
		cur := bucket.Countries[norm]
		cur.Requests += d.Requests
		cur.DurationMsSum += d.DurationMsSum
		bucket.Countries[norm] = cur
	}
	bucket.UpdatedAtMs = nowMs
	cache_service.SaveGeoIPBucket(ctx, dc, bucket, geoIPAggregateTTL)

	var total int64
	for _, d := range bucket.Countries {
		total += d.Requests
	}
	if total == 0 {
		return infra_telemetry_models.GeoIP24hSnapshot{
			TopCountries: []infra_telemetry_models.GeoIPCountryStat{},
		}
	}

	out := make([]infra_telemetry_models.GeoIPCountryStat, 0, len(bucket.Countries))
	for cc, d := range bucket.Countries {
		if d.Requests <= 0 {
			continue
		}
		out = append(out, infra_telemetry_models.GeoIPCountryStat{
			CountryCode: cc,
			Requests:    d.Requests,
			AvgP95Ms:    round2(d.DurationMsSum / float64(d.Requests)),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Requests != out[j].Requests {
			return out[i].Requests > out[j].Requests
		}
		return out[i].CountryCode < out[j].CountryCode
	})

	suggestion := ""
	for _, stat := range out {
		share := float64(stat.Requests) / float64(total)
		if stat.CountryCode != "ZZ" && share >= 0.15 && stat.AvgP95Ms > 150.0 {
			suggestion = fmt.Sprintf("Consider opening a datacenter closer to %s (%.1f%% of 24h traffic, avg latency %.0fms)", stat.CountryCode, share*100, stat.AvgP95Ms)
			break
		}
	}

	return infra_telemetry_models.GeoIP24hSnapshot{
		TopCountries:        out,
		ExpansionSuggestion: suggestion,
	}
}

func deriveBottleneckHint(cpu, p95, workerSat float64, mqDepth int64, mqLag, wsCap, pgWrite, cuckooFill float64) string {
	switch {
	case mqLag > 20 || (workerSat >= 0.85 && mqDepth > 10000):
		return fmt.Sprintf("Write-Behind backlog (%d evts, lag %.1fs): increase WORKER_CONCURRENCY or add a worker node", mqDepth, mqLag)
	case pgWrite >= 0.85:
		return fmt.Sprintf("PostgreSQL write pool saturated (%.0f%%): check slow queries or offload reads to replica", pgWrite*100)
	case p95 > 500:
		return fmt.Sprintf("High API p95 latency (%.0fms): inspect DB pool or scale api_node pods", p95)
	case wsCap >= 0.85:
		return fmt.Sprintf("WebSocket concurrency nearing ceiling (%.0f%%): scale ws_node pods", wsCap*100)
	case cpu >= 0.85:
		return fmt.Sprintf("High CPU utilization (%.0f%%): offload heavy workloads or add compute node", cpu*100)
	case cuckooFill >= 0.80:
		return fmt.Sprintf("Cuckoo filter fill ratio high (%.0f%%): schedule cuckoo_filter_health_and_rebuild", cuckooFill*100)
	default:
		return ""
	}
}

func maxFloat(vals ...float64) float64 {
	m := 0.0
	for _, v := range vals {
		if v > m {
			m = v
		}
	}
	return clamp01(m)
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

func envOrDefault(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	return v
}

func splitCSVEnv(k string) []string {
	raw := strings.TrimSpace(os.Getenv(k))
	if raw == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
