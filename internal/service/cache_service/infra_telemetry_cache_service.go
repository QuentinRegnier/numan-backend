package cache_service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/infra_telemetry_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// SavePodHeartbeat persiste le heartbeat d'un pod dans Redis Core (`infra:telemetry:dc:<dc>:pod:<pod>`).
func SavePodHeartbeat(ctx context.Context, hb infra_telemetry_models.PodHeartbeat, ttl time.Duration) error {
	if repoRedis.InfraTelemetry == nil {
		return nil
	}
	raw, err := json.Marshal(hb)
	if err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeInternalError, "Échec de sérialisation du heartbeat pod.", err)
	}
	keySuffix := fmt.Sprintf("dc:%s:pod:%s", hb.Datacenter, hb.PodName)
	if err := repoRedis.InfraTelemetry.SetRawWithTTL(ctx, keySuffix, raw, ttl); err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Échec d'écriture du heartbeat pod dans Redis Core.", err)
	}
	return nil
}

// ListActivePodHeartbeats scanne et lit en pipeline tous les heartbeats actifs d'un datacenter dans Redis Core.
func ListActivePodHeartbeats(ctx context.Context, datacenter string) ([]infra_telemetry_models.PodHeartbeat, error) {
	if repoRedis.InfraTelemetry == nil {
		return nil, nil
	}
	pattern := repoRedis.InfraTelemetry.Key(fmt.Sprintf("dc:%s:pod:*", datacenter))
	keys, err := repoRedis.ScanKeysOnCore(ctx, pattern, 200)
	if err != nil {
		return nil, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Échec du scan des heartbeats pods dans Redis Core.", err)
	}
	if len(keys) == 0 {
		return nil, nil
	}

	rawList, err := repoRedis.GetBytesBatchOnCore(ctx, keys)
	if err != nil {
		return nil, numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Échec de lecture pipeline des heartbeats pods.", err)
	}
	out := make([]infra_telemetry_models.PodHeartbeat, 0, len(rawList))
	for _, raw := range rawList {
		var hb infra_telemetry_models.PodHeartbeat
		if json.Unmarshal(raw, &hb) == nil {
			out = append(out, hb)
		}
	}
	return out, nil
}

// GetLiveTelemetrySnapshot lit le snapshot télémétrie unifié du datacenter depuis Redis Core (<1ms).
func GetLiveTelemetrySnapshot(ctx context.Context, datacenter string) (*infra_telemetry_models.CLITelemetrySnapshot, error) {
	if repoRedis.InfraTelemetry == nil {
		return nil, nil
	}
	raw, err := repoRedis.InfraTelemetry.GetBytes(ctx, fmt.Sprintf("dc:%s:live", datacenter))
	if err != nil || len(raw) == 0 {
		return nil, nil
	}
	var snap infra_telemetry_models.CLITelemetrySnapshot
	if errU := json.Unmarshal(raw, &snap); errU != nil {
		return nil, nil
	}
	return &snap, nil
}

// SaveLiveTelemetrySnapshot écrit le snapshot télémétrie unifié du datacenter dans Redis Core.
func SaveLiveTelemetrySnapshot(ctx context.Context, datacenter string, snap *infra_telemetry_models.CLITelemetrySnapshot, ttl time.Duration) error {
	if repoRedis.InfraTelemetry == nil || snap == nil {
		return nil
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeInternalError, "Échec de sérialisation du snapshot télémétrie.", err)
	}
	if err := repoRedis.InfraTelemetry.SetRawWithTTL(ctx, fmt.Sprintf("dc:%s:live", datacenter), raw, ttl); err != nil {
		return numan_error.NewAppError(http.StatusInternalServerError, numan_error.CodeCacheError, "Échec d'écriture du snapshot télémétrie dans Redis Core.", err)
	}
	return nil
}

// GetHourlyProfile lit le profil horaire glissant 7j du datacenter depuis Redis Core.
func GetHourlyProfile(ctx context.Context, datacenter string) infra_telemetry_models.HourlyProfile {
	var prof infra_telemetry_models.HourlyProfile
	if repoRedis.InfraTelemetry == nil {
		return prof
	}
	raw, err := repoRedis.InfraTelemetry.GetBytes(ctx, fmt.Sprintf("dc:%s:hourly_profile", datacenter))
	if err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &prof)
	}
	return prof
}

// SaveHourlyProfile persiste le profil horaire glissant 7j du datacenter dans Redis Core.
func SaveHourlyProfile(ctx context.Context, datacenter string, prof infra_telemetry_models.HourlyProfile, ttl time.Duration) {
	if repoRedis.InfraTelemetry == nil {
		return
	}
	if raw, err := json.Marshal(prof); err == nil {
		_ = repoRedis.InfraTelemetry.SetRawWithTTL(ctx, fmt.Sprintf("dc:%s:hourly_profile", datacenter), raw, ttl)
	}
}

// GetGeoIPBucket lit l'agrégat ISO-2 24h depuis Redis Core.
func GetGeoIPBucket(ctx context.Context, datacenter string) infra_telemetry_models.GeoIPBucket {
	bucket := infra_telemetry_models.GeoIPBucket{
		Countries: make(map[string]infra_telemetry_models.CountryDelta),
	}
	if repoRedis.InfraTelemetry == nil {
		return bucket
	}
	raw, err := repoRedis.InfraTelemetry.GetBytes(ctx, fmt.Sprintf("dc:%s:geoip:24h", datacenter))
	if err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &bucket)
		if bucket.Countries == nil {
			bucket.Countries = make(map[string]infra_telemetry_models.CountryDelta)
		}
	}
	return bucket
}

// SaveGeoIPBucket persiste l'agrégat ISO-2 24h dans Redis Core.
func SaveGeoIPBucket(ctx context.Context, datacenter string, bucket infra_telemetry_models.GeoIPBucket, ttl time.Duration) {
	if repoRedis.InfraTelemetry == nil || len(bucket.Countries) == 0 {
		return
	}
	if raw, err := json.Marshal(bucket); err == nil {
		_ = repoRedis.InfraTelemetry.SetRawWithTTL(ctx, fmt.Sprintf("dc:%s:geoip:24h", datacenter), raw, ttl)
	}
}

// AppendTelemetryHistory1m insère un point 1-minute dans le ZSET borné (1440 points) et retourne les derniers points.
func AppendTelemetryHistory1m(ctx context.Context, datacenter string, sample infra_telemetry_models.HistorySample1m, maxSamples int64, returnLimit int64) []infra_telemetry_models.HistorySample1m {
	if repoRedis.InfraTelemetry == nil {
		return []infra_telemetry_models.HistorySample1m{sample}
	}
	histKey := fmt.Sprintf("dc:%s:history:1m", datacenter)
	minuteBucket := (sample.TimestampMs / 60000) * 60000
	sample.TimestampMs = minuteBucket

	if raw, err := json.Marshal(sample); err == nil {
		scoreStr := fmt.Sprintf("%d", minuteBucket)
		_ = repoRedis.InfraTelemetry.ZRemRangeByScore(ctx, histKey, scoreStr, scoreStr)
		_ = repoRedis.InfraTelemetry.ZAddWithCap(ctx, histKey, float64(minuteBucket), string(raw), int(maxSamples))
	}

	if returnLimit <= 0 {
		returnLimit = 60
	}
	members, err := repoRedis.InfraTelemetry.ZRevRange(ctx, histKey, 0, returnLimit-1)
	if err != nil || len(members) == 0 {
		return []infra_telemetry_models.HistorySample1m{sample}
	}
	out := make([]infra_telemetry_models.HistorySample1m, 0, len(members))
	for i := len(members) - 1; i >= 0; i-- {
		var s infra_telemetry_models.HistorySample1m
		if json.Unmarshal([]byte(members[i]), &s) == nil {
			out = append(out, s)
		}
	}
	return out
}

// AcquireTelemetryAggregatorLock acquiert le verrou distribué Redis Core pour l'agrégateur télémétrie.
func AcquireTelemetryAggregatorLock(ctx context.Context, ttl time.Duration) (string, bool, error) {
	return repoRedis.AcquireCronLock(ctx, "telemetry_agg", ttl)
}

// ReleaseTelemetryAggregatorLock libère le verrou distribué Redis Core de l'agrégateur télémétrie.
func ReleaseTelemetryAggregatorLock(ctx context.Context, token string) {
	if token == "" {
		return
	}
	_ = repoRedis.ReleaseCronLock(ctx, "telemetry_agg", token)
}

// CollectWriteBehindQueueStats mesure la profondeur totale des 64 shards Write-Behind, l'âge du plus vieil événement et la taille de la DLQ.
func CollectWriteBehindQueueStats(ctx context.Context) (depth int64, oldestAgeSec float64, dlqSize int64) {
	if d, err := repoRedis.TotalQueueDepth(ctx); err == nil {
		depth = d
	}
	if age, err := repoRedis.OldestQueueEventAgeSeconds(ctx); err == nil {
		oldestAgeSec = age
	}
	if repoRedis.DLQ != nil {
		if n, err := repoRedis.DLQ.LLen(ctx, "global"); err == nil {
			dlqSize = n
		}
	}
	return depth, oldestAgeSec, dlqSize
}

// CollectRedis3TierSnapshot collecte et normalise la télémétrie temps réel des 3 tiers Redis (Core, Object Pool, Pub/Sub).
func CollectRedis3TierSnapshot(ctx context.Context) infra_telemetry_models.Redis3TierSnapshot {
	coreTel, objTel, psTel := repoRedis.CollectAllTiersTelemetry(ctx)
	return infra_telemetry_models.Redis3TierSnapshot{
		Core:       mapTierTelemetry(coreTel, false),
		ObjectPool: mapTierTelemetry(objTel, !objTel.Dedicated),
		PubSub:     mapTierTelemetry(psTel, !psTel.Dedicated),
	}
}

func mapTierTelemetry(tel infraRedis.TierTelemetrySnapshot, sharedWithCore bool) infra_telemetry_models.RedisTierSnapshot {
	details := make([]infra_telemetry_models.RedisShardSnapshot, 0, len(tel.Shards))
	nodeSet := make(map[string]struct{}, len(tel.Shards))
	replicas := 0
	for _, s := range tel.Shards {
		if s.Role == "replica" || s.Role == "slave" {
			replicas++
		}
		if s.NodeHostname != "" {
			nodeSet[s.NodeHostname] = struct{}{}
		}
		details = append(details, infra_telemetry_models.RedisShardSnapshot{
			ShardID:          s.ShardID,
			Role:             s.Role,
			Node:             s.NodeHostname,
			UsedMemoryBytes:  s.UsedBytes,
			MaxMemoryBytes:   s.MaxBytes,
			FillRatio:        math.Round(s.FillRatio*10000) / 10000,
			ConnectedClients: s.ConnectedClients,
			EvictedKeys:      s.EvictedKeys,
		})
	}
	nodes := make([]string, 0, len(nodeSet))
	for n := range nodeSet {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	fillRatio := 0.0
	if tel.TotalMaxBytes > 0 {
		fillRatio = math.Round((float64(tel.TotalUsedBytes)/float64(tel.TotalMaxBytes))*10000) / 10000
	}

	return infra_telemetry_models.RedisTierSnapshot{
		Mode:               tel.Mode,
		SharedWithCore:     sharedWithCore,
		Shards:             len(tel.Shards),
		Replicas:           replicas,
		UsedBytes:          tel.TotalUsedBytes,
		MaxBytes:           tel.TotalMaxBytes,
		FillRatio:          fillRatio,
		EvictedKeys:        tel.TotalEvictedKeys,
		ShardedPubSub:      tel.Tier == "pubsub" && tel.Mode == "cluster",
		Channels:           tel.PubSubChannels,
		ParticipatingNodes: nodes,
		ShardDetails:       details,
	}
}
