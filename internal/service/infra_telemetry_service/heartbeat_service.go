package infra_telemetry_service

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/infra_telemetry_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/metrics"
	repoPostgres "github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
)

const (
	defaultHeartbeatInterval = 10 * time.Second
	defaultHeartbeatTTL      = 35 * time.Second
	defaultWSCapacityLimit   = 50000
)

// HeartbeatConfig configure le publisher de télémétrie par pod.
type HeartbeatConfig struct {
	DatacenterID    string
	PodName         string
	Hostname        string
	Role            string
	NodeID          int64
	Interval        time.Duration
	TTL             time.Duration
	WSStatsFn       func() (activeUsers int64, activeConnections int64)
	CuckooFillFn    func() (float64, bool)
	WSCapacityLimit int64
}

// StartPodHeartbeatPublisher lance la publication périodique (10s) du heartbeat pod dans Redis Core via cache_service.
func StartPodHeartbeatPublisher(parentCtx context.Context, cfg HeartbeatConfig) func() {
	cfg = normalizeHeartbeatConfig(cfg)

	ctx, cancel := context.WithCancel(parentCtx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		_ = PublishPodHeartbeatOnce(ctx, cfg)
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := PublishPodHeartbeatOnce(ctx, cfg); err != nil {
					numan_log.Warn(ctx).Err(err).Str("pod", cfg.PodName).Msg("Échec publication heartbeat télémétrie pod")
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

// PublishPodHeartbeatOnce collecte les métriques locales du pod et les persiste dans Redis Core via cache_service.
func PublishPodHeartbeatOnce(ctx context.Context, cfg HeartbeatConfig) error {
	cfg = normalizeHeartbeatConfig(cfg)
	hb := CollectPodHeartbeat(ctx, cfg)
	if err := hb.Validate(); err != nil {
		return err
	}
	return cache_service.SavePodHeartbeat(ctx, hb, cfg.TTL)
}

// CollectPodHeartbeat assemble un snapshot léger des métriques locales du pod.
func CollectPodHeartbeat(ctx context.Context, cfg HeartbeatConfig) infra_telemetry_models.PodHeartbeat {
	cfg = normalizeHeartbeatConfig(cfg)

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	rps, inFlight, p95 := metrics.Default().SnapshotHTTPStats()
	geoDelta := metrics.Default().DrainGeoIPDelta()
	workerConc, workerActive, workerSat := metrics.Default().WorkerSaturation()

	hb := infra_telemetry_models.PodHeartbeat{
		Datacenter:              cfg.DatacenterID,
		PodName:                 cfg.PodName,
		Hostname:                cfg.Hostname,
		Role:                    cfg.Role,
		NodeID:                  cfg.NodeID,
		TimestampMs:             domain.NowMillis(),
		GoCPUUsageRatio:         estimateGoCPURatio(),
		GoMemoryAllocBytes:      mem.Alloc,
		Goroutines:              runtime.NumGoroutine(),
		HTTPRequestsPerSec:      rps,
		HTTPInFlight:            inFlight,
		HTTPP95LatencyMs:        p95,
		WorkerConcurrency:       workerConc,
		WorkerActiveFlushes:     workerActive,
		WorkerSaturationRatio:   workerSat,
		GeoIPCountryCountsDelta: geoDelta,
	}

	if cfg.WSStatsFn != nil {
		users, conns := cfg.WSStatsFn()
		hb.WSActiveUsers = users
		hb.WSActiveConnections = conns
		hb.WSCapacityRatio = clamp01(float64(conns) / float64(cfg.WSCapacityLimit))
	}

	if cfg.CuckooFillFn != nil {
		if fill, ok := cfg.CuckooFillFn(); ok {
			hb.CuckooFillRatio = fill
		}
	}

	// Collecter profondeur Write-Behind + plus vieil événement + DLQ uniquement sur worker/monolith/scheduler
	if cfg.Role == "monolith" || cfg.Role == "worker_node" || cfg.Role == "scheduler_node" {
		qCtx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
		hb.QueueDepthTotal, hb.QueueOldestEventAgeSec, hb.DLQSize = cache_service.CollectWriteBehindQueueStats(qCtx)
		cancel()
	}

	if st, ok := repoPostgres.FuncWritePoolStats(); ok {
		maxOpen := st.MaxOpenConnections
		if maxOpen <= 0 {
			maxOpen = 25
		}
		hb.PostgresWritePoolUtil = clamp01(float64(st.InUse) / float64(maxOpen))
	}
	if st, ok := repoPostgres.FuncReadPoolStats(); ok {
		maxOpen := st.MaxOpenConnections
		if maxOpen <= 0 {
			maxOpen = 25
		}
		hb.PostgresReadPoolUtil = clamp01(float64(st.InUse) / float64(maxOpen))
	}

	return hb
}

func normalizeHeartbeatConfig(cfg HeartbeatConfig) HeartbeatConfig {
	if strings.TrimSpace(cfg.DatacenterID) == "" {
		cfg.DatacenterID = "default"
	}
	if strings.TrimSpace(cfg.PodName) == "" {
		cfg.PodName = strings.TrimSpace(os.Getenv("POD_NAME"))
	}
	if cfg.PodName == "" {
		if h, err := os.Hostname(); err == nil && h != "" {
			cfg.PodName = h
		} else {
			cfg.PodName = fmt.Sprintf("%s-%d", cfg.Role, cfg.NodeID)
		}
	}
	if strings.TrimSpace(cfg.Hostname) == "" {
		cfg.Hostname = strings.TrimSpace(os.Getenv("NODE_NAME"))
	}
	if cfg.Hostname == "" {
		if h, err := os.Hostname(); err == nil && h != "" {
			cfg.Hostname = h
		} else {
			cfg.Hostname = "local-node"
		}
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultHeartbeatInterval
	}
	if cfg.TTL <= 0 {
		cfg.TTL = defaultHeartbeatTTL
	}
	if cfg.WSCapacityLimit <= 0 {
		cfg.WSCapacityLimit = defaultWSCapacityLimit
	}
	return cfg
}

func estimateGoCPURatio() float64 {
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			if load1, errP := strconv.ParseFloat(fields[0], 64); errP == nil {
				cpus := float64(runtime.GOMAXPROCS(0))
				if cpus < 1 {
					cpus = 1
				}
				return clamp01(load1 / cpus)
			}
		}
	}
	cpus := float64(runtime.GOMAXPROCS(0))
	if cpus < 1 {
		cpus = 1
	}
	return clamp01(float64(runtime.NumGoroutine()) / (cpus * 250.0))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
