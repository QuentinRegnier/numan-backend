package infra_telemetry_models

import (
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// CountryDelta représente les statistiques agrégées par pays (ISO-3166 alpha-2) depuis le dernier tick.
// Aucune adresse IP brute n'est jamais stockée ni journalisée.
type CountryDelta struct {
	Requests      int64   `json:"requests"`
	DurationMsSum float64 `json:"duration_ms_sum"`
}

// NormalizeCountryCode valide et normalise un code pays ISO-3166 alpha-2 en majuscules.
// Retourne "ZZ" si absent ou invalide. Aucune IP n'est jamais acceptée ni stockée.
func NormalizeCountryCode(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if len(s) != 2 {
		return "ZZ"
	}
	if s[0] < 'A' || s[0] > 'Z' || s[1] < 'A' || s[1] > 'Z' {
		return "ZZ"
	}
	if s == "XX" || s == "T1" {
		return "ZZ"
	}
	return s
}

// RedisShardMetric décrit la consommation mémoire d'un shard Redis pour l'exposition Prometheus.
type RedisShardMetric struct {
	Tier             string
	ShardID          string
	Role             string
	Node             string
	UsedMemoryBytes  int64
	MaxMemoryBytes   int64
	FillRatio        float64
	ConnectedClients int64
}

// RedisTierMetricSnapshot agrège l'état d'un tier Redis pour Prometheus.
type RedisTierMetricSnapshot struct {
	Tier            string
	Mode            string
	EvictedKeys     int64
	PubSubChannels  int64
	PubSubMsgPerSec float64
	Shards          []RedisShardMetric
}

// SchedulerJobMetric résume l'état Prometheus d'un job planifié.
type SchedulerJobMetric struct {
	Name            string
	Running         bool
	LastDurationSec float64
}

// SchedulerMetricSnapshot agrège l'état du scheduler pour Prometheus.
type SchedulerMetricSnapshot struct {
	Datacenter      string
	Regime          string
	StarvationAlert bool
	Jobs            []SchedulerJobMetric
}

// PodHeartbeat est le payload compact publié toutes les 10s par chaque pod vers Redis Core
// (`infra:telemetry:dc:<dc_id>:pod:<pod_name>`, TTL 30s).
type PodHeartbeat struct {
	Datacenter              string                  `json:"datacenter"`
	PodName                 string                  `json:"pod_name"`
	Hostname                string                  `json:"hostname"`
	Role                    string                  `json:"role"`
	NodeID                  int64                   `json:"node_id"`
	TimestampMs             int64                   `json:"ts_ms"`
	GoCPUUsageRatio         float64                 `json:"go_cpu_usage_ratio"`
	GoMemoryAllocBytes      uint64                  `json:"go_memory_alloc_bytes"`
	Goroutines              int                     `json:"goroutines"`
	HTTPRequestsPerSec      float64                 `json:"http_requests_per_sec"`
	HTTPInFlight            int64                   `json:"http_in_flight"`
	HTTPP95LatencyMs        float64                 `json:"http_p95_latency_ms"`
	WSActiveConnections     int64                   `json:"ws_active_connections"`
	WSActiveUsers           int64                   `json:"ws_active_users"`
	WSCapacityRatio         float64                 `json:"ws_capacity_ratio"`
	WorkerConcurrency       int64                   `json:"worker_concurrency"`
	WorkerActiveFlushes     int64                   `json:"worker_active_flushes"`
	WorkerSaturationRatio   float64                 `json:"worker_saturation_ratio"`
	QueueDepthTotal         int64                   `json:"queue_depth_total"`
	QueueOldestEventAgeSec  float64                 `json:"queue_oldest_event_age_sec"`
	DLQSize                 int64                   `json:"dlq_size"`
	CuckooFillRatio         float64                 `json:"cuckoo_fill_ratio"`
	PostgresWritePoolUtil   float64                 `json:"postgres_write_pool_util"`
	PostgresReadPoolUtil    float64                 `json:"postgres_read_pool_util"`
	GeoIPCountryCountsDelta map[string]CountryDelta `json:"geoip_country_counts_delta,omitempty"`
}

// Validate vérifie la validité minimale d'un heartbeat de pod.
func (p *PodHeartbeat) Validate() error {
	if p == nil || strings.TrimSpace(p.Datacenter) == "" || strings.TrimSpace(p.PodName) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le heartbeat de pod requiert datacenter et pod_name.", nil)
	}
	return nil
}

// ServerStressSnapshot résume le stress agrégé d'un serveur physique (pour drill-down numan-cli / Admin Panel).
type ServerStressSnapshot struct {
	Hostname          string   `json:"hostname"`
	Roles             []string `json:"roles"`
	CPURatio          float64  `json:"cpu_ratio"`
	RAMRatio          float64  `json:"ram_ratio"`
	RPS               float64  `json:"rps"`
	P95Ms             float64  `json:"p95_ms"`
	WSConns           int64    `json:"ws_conns"`
	WorkerConcurrency int64    `json:"worker_concurrency"`
	WorkerSaturation  float64  `json:"worker_saturation"`
	MQDepth           int64    `json:"mq_depth"`
	MQLagSec          float64  `json:"mq_lag_sec"`
	DLQSize           int64    `json:"dlq_size"`
	CuckooFillRatio   float64  `json:"cuckoo_fill_ratio"`
	BottleneckHint    string   `json:"bottleneck_hint"`
}

// RedisShardSnapshot décrit un nœud/shard Redis individuel au sein d'un tier.
type RedisShardSnapshot struct {
	ShardID          string  `json:"shard_id"`
	Role             string  `json:"role"`
	Node             string  `json:"node"`
	UsedMemoryBytes  int64   `json:"used_memory_bytes"`
	MaxMemoryBytes   int64   `json:"max_memory_bytes"`
	FillRatio        float64 `json:"fill_ratio"`
	ConnectedClients int64   `json:"connected_clients"`
	EvictedKeys      int64   `json:"evicted_keys"`
}

// RedisTierSnapshot résume un tier logique Redis (Core, Object Pool, Pub/Sub).
type RedisTierSnapshot struct {
	Mode               string               `json:"mode"`
	SharedWithCore     bool                 `json:"shared_with_core,omitempty"`
	Shards             int                  `json:"shards"`
	Replicas           int                  `json:"replicas"`
	UsedBytes          int64                `json:"used_bytes"`
	MaxBytes           int64                `json:"max_bytes"`
	FillRatio          float64              `json:"fill_ratio"`
	EvictedKeys        int64                `json:"evicted_keys"`
	ShardedPubSub      bool                 `json:"sharded_pubsub,omitempty"`
	Channels           int64                `json:"channels,omitempty"`
	MsgPerSec          float64              `json:"msg_per_sec,omitempty"`
	HitRatio           float64              `json:"hit_ratio,omitempty"`
	ParticipatingNodes []string             `json:"participating_nodes,omitempty"`
	ShardDetails       []RedisShardSnapshot `json:"shard_details,omitempty"`
}

// Redis3TierSnapshot regroupe les 3 tiers Redis du datacenter.
type Redis3TierSnapshot struct {
	Core       RedisTierSnapshot `json:"core"`
	ObjectPool RedisTierSnapshot `json:"object_pool"`
	PubSub     RedisTierSnapshot `json:"pubsub"`
}

// PostgresTopologySnapshot résume la santé des pools PostgreSQL.
type PostgresTopologySnapshot struct {
	Primary       string   `json:"primary"`
	Replicas      []string `json:"replicas"`
	PoolWriteUtil float64  `json:"pool_write_util"`
	PoolReadUtil  float64  `json:"pool_read_util"`
}

// MongoTopologySnapshot résume la topologie MongoDB L2.
type MongoTopologySnapshot struct {
	Primary     string   `json:"primary"`
	Secondaries []string `json:"secondaries"`
	LagSec      float64  `json:"lag_sec"`
}

// DatabasesSnapshot regroupe Postgres et MongoDB.
type DatabasesSnapshot struct {
	Postgres PostgresTopologySnapshot `json:"postgres"`
	Mongo    MongoTopologySnapshot    `json:"mongo"`
}

// SchedulerSnapshot résume l'état du Scheduler intégré au payload CLI / Admin.
type SchedulerSnapshot struct {
	Paused       bool                         `json:"paused"`
	PauseReason  string                       `json:"pause_reason,omitempty"`
	ActiveJob    string                       `json:"active_job,omitempty"`
	ProgressPct  float64                      `json:"progress_pct"`
	CPUBudgetPct int                          `json:"cpu_budget_pct"`
	JobsSummary  []scheduler_models.JobStatus `json:"jobs_summary"`
}

// GeoIPCountryStat représente un pays agrégé (STRICTEMENT code pays ISO-2, ZÉRO IP brute).
type GeoIPCountryStat struct {
	CountryCode string  `json:"cc"`
	Requests    int64   `json:"requests"`
	AvgP95Ms    float64 `json:"avg_p95_ms"`
}

// GeoIP24hSnapshot agrège le trafic par pays sur 24h et génère une recommandation d'expansion DC si latence > 150ms.
type GeoIP24hSnapshot struct {
	TopCountries        []GeoIPCountryStat `json:"top_countries"`
	ExpansionSuggestion string             `json:"expansion_suggestion,omitempty"`
}

// HistorySample1m est un point 1-minute dans le ring buffer 24h (1440 points).
type HistorySample1m struct {
	TimestampMs int64   `json:"ts_ms"`
	Activity    float64 `json:"activity"`
	Saturation  float64 `json:"saturation"`
	RPS         float64 `json:"rps"`
	Regime      string  `json:"regime"`
}

// HourlyProfile stocke la moyenne glissante (EWMA 7j) de l'activité pour chacune des 24 heures locales du DC.
type HourlyProfile struct {
	Hours       [24]float64 `json:"hours"`
	SampleCount [24]int64   `json:"sample_count"`
	UpdatedAtMs int64       `json:"updated_at_ms"`
}

// GeoIPBucket persiste l'agrégat ISO-2 de la fenêtre 24h dans Redis Core.
type GeoIPBucket struct {
	UpdatedAtMs int64                   `json:"updated_at_ms"`
	Countries   map[string]CountryDelta `json:"countries"`
}

// CLITelemetrySnapshot est le JSON unifié stocké dans Redis Core (`infra:telemetry:dc:<dc_id>:live`)
// et redistribué via les 3 canaux (Redis Core, Admin Panel `/admin/telemetry/overview`, `:9090/internal/cli-telemetry`).
type CLITelemetrySnapshot struct {
	Datacenter               string                  `json:"datacenter"`
	TimestampMs              int64                   `json:"timestamp_ms"`
	DCSaturationRatio        float64                 `json:"dc_saturation_ratio"`
	DCActivityScore          float64                 `json:"dc_activity_score"`
	SchedulerRegime          scheduler_models.Regime `json:"scheduler_regime"`
	OffPeakSaturationWarning bool                    `json:"off_peak_saturation_warning"`
	Starvation48hWarning     bool                    `json:"starvation_48h_warning"`
	HourlyBaseline24h        [24]float64             `json:"hourly_baseline_24h"`
	Scheduler                SchedulerSnapshot       `json:"scheduler"`
	RedisTiers               Redis3TierSnapshot      `json:"redis_tiers"`
	Databases                DatabasesSnapshot       `json:"databases"`
	Servers                  []ServerStressSnapshot  `json:"servers"`
	GeoIP24h                 GeoIP24hSnapshot        `json:"geoip_24h"`
	History1m                []HistorySample1m       `json:"history_1m,omitempty"`
}

// Validate vérifie l'intégrité du snapshot télémétrie datacenter.
func (s *CLITelemetrySnapshot) Validate() error {
	if s == nil || strings.TrimSpace(s.Datacenter) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le snapshot télémétrie datacenter est invalide.", nil)
	}
	if !s.SchedulerRegime.IsValid() {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le régime scheduler du snapshot télémétrie est invalide.", nil)
	}
	return nil
}
