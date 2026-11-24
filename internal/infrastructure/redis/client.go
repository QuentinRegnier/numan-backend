package redis

import (
	"context"
	"crypto/tls"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/redis/go-redis/v9"
)

const (
	defaultRedisAddr         = "redis:6379"
	defaultRedisMasterName   = "numan-master"
	defaultRedisMaxRetries   = 3
	defaultRedisDialTimeout  = 5 * time.Second
	defaultRedisReadTimeout  = 3 * time.Second
	defaultRedisWriteTimeout = 3 * time.Second
	defaultRedisPoolSize     = 50
)

const (
	TopologyModeStandalone = "standalone"
	TopologyModeSentinel   = "sentinel"
	TopologyModeCluster    = "cluster"
)

var (
	// Rdb est l'alias rétrocompatible de RdbCore (Tier 1 : état HA répliqué).
	// NOTE CRITIQUE COHÉRENCE : RdbCore cible le Master par défaut afin de garantir
	// une cohérence stricte pour les Tombstones, les files Write-Behind (EnqueueDB),
	// les verrous distribués (AcquireCronLock) et le ratchet HMAC des sessions.
	Rdb redis.UniversalClient

	// RdbCore (Tier 1) — High-Value Replicated HA (Standalone, Sentinel HA ou Cluster répliqué + AOF/RDB).
	RdbCore redis.UniversalClient

	// RdbObjects (Tier 2) — Bulk Ephemeral L1 Object Cache (100% RAM utilisable, Cluster non-répliqué, sans AOF, volatile-lfu).
	// Alias automatique vers RdbCore lorsque REDIS_OBJECT_* n'est pas configuré (0 surcoût sur 1 serveur).
	RdbObjects redis.UniversalClient

	// RdbPubSub (Tier 3) — Bus éphémère ultra-basse latence pour WebSockets & canaux de sync (Sharded Pub/Sub en Cluster).
	// Alias automatique vers RdbCore lorsque REDIS_PUBSUB_* n'est pas configuré (0 surcoût sur 1 serveur).
	RdbPubSub redis.UniversalClient
)

// RedisTierConfig regroupe la configuration résolue d'un tier Redis (Standalone, Sentinel HA ou Cluster).
type RedisTierConfig struct {
	Prefix           string
	Addr             string
	ClusterAddrs     []string
	MasterName       string
	SentinelAddrs    []string
	Password         string
	SentinelPassword string
	DB               int
	TLSConfig        *tls.Config
	MaxRetries       int
	DialTimeout      time.Duration
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	PoolSize         int
	RouteByLatency   bool
	RouteRandomly    bool
}

// RedisConfig est conservé comme alias de RedisTierConfig pour une rétrocompatibilité 100 %.
type RedisConfig = RedisTierConfig

// IsCluster indique si le tier utilise le mode Redis Cluster natif.
func (c RedisTierConfig) IsCluster() bool {
	return len(c.ClusterAddrs) > 0
}

// IsSentinel indique si le mode Haute Disponibilité Redis Sentinel est actif (et non Cluster).
func (c RedisTierConfig) IsSentinel() bool {
	return !c.IsCluster() && len(c.SentinelAddrs) > 0
}

// IsConfigured indique si au moins une adresse (Cluster, Sentinel ou Standalone) a été fournie.
func (c RedisTierConfig) IsConfigured() bool {
	return len(c.ClusterAddrs) > 0 || len(c.SentinelAddrs) > 0 || strings.TrimSpace(c.Addr) != ""
}

// Mode retourne le mode topologique résolu ("cluster", "sentinel" ou "standalone").
func (c RedisTierConfig) Mode() string {
	if c.IsCluster() {
		return TopologyModeCluster
	}
	if c.IsSentinel() {
		return TopologyModeSentinel
	}
	return TopologyModeStandalone
}

// CoreClient retourne le client Tier 1 (Redis Core).
// Si Rdb a été explicitement mis à nil (ex: tests simulant Redis indisponible), CoreClient retourne nil.
func CoreClient() redis.UniversalClient {
	if Rdb == nil {
		return nil
	}
	return Rdb
}

// ObjectClient retourne le client Tier 2 (Redis Object Pool), avec repli automatique sur CoreClient.
func ObjectClient() redis.UniversalClient {
	if RdbObjects != nil {
		return RdbObjects
	}
	return CoreClient()
}

// PubSubClient retourne le client Tier 3 (Redis Pub/Sub), avec repli automatique sur CoreClient.
func PubSubClient() redis.UniversalClient {
	if RdbPubSub != nil {
		return RdbPubSub
	}
	return CoreClient()
}

// HasDedicatedObjectPool indique si Tier 2 utilise une instance/cluster distinct de Tier 1.
func HasDedicatedObjectPool() bool {
	core := CoreClient()
	return RdbObjects != nil && core != nil && RdbObjects != core
}

// HasDedicatedPubSub indique si Tier 3 utilise une instance/cluster distinct de Tier 1.
func HasDedicatedPubSub() bool {
	core := CoreClient()
	return RdbPubSub != nil && core != nil && RdbPubSub != core
}

// IsClusterClient retourne true si le client universel cible un Redis Cluster (*redis.ClusterClient).
func IsClusterClient(c redis.UniversalClient) bool {
	if c == nil {
		return false
	}
	_, ok := c.(*redis.ClusterClient)
	return ok
}

// TierTopologyMode identifie le mode topologique d'un client universel ("standalone", "sentinel", "cluster").
func TierTopologyMode(c redis.UniversalClient) string {
	if c == nil {
		return TopologyModeStandalone
	}
	if _, ok := c.(*redis.ClusterClient); ok {
		return TopologyModeCluster
	}
	if client, ok := c.(*redis.Client); ok {
		opts := client.Options()
		if opts != nil && opts.Addr == "FailoverClient" {
			return TopologyModeSentinel
		}
	}
	return TopologyModeStandalone
}

func parsePositiveIntEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func parsePositiveSecondsEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return time.Duration(v) * time.Second
}

func parseCommaAddrs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	addrs := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			addrs = append(addrs, trimmed)
		}
	}
	return addrs
}

func envWithFallback(primaryKey, fallbackKey string) string {
	if v := strings.TrimSpace(os.Getenv(primaryKey)); v != "" {
		return v
	}
	if fallbackKey != "" {
		return strings.TrimSpace(os.Getenv(fallbackKey))
	}
	return ""
}

func resolveTierTLS(prefix, fallbackHostAddr string) *tls.Config {
	tlsFlag := envWithFallback(prefix+"_TLS", "REDIS_TLS")
	if !strings.EqualFold(tlsFlag, "true") {
		return nil
	}
	serverName := envWithFallback(prefix+"_TLS_SERVER_NAME", "REDIS_TLS_SERVER_NAME")
	if serverName == "" && fallbackHostAddr != "" {
		if host, _, err := net.SplitHostPort(fallbackHostAddr); err == nil {
			serverName = host
		} else {
			serverName = fallbackHostAddr
		}
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: serverName,
	}
}

// ResolveTierConfig résout la configuration complète d'un tier Redis à partir du préfixe ("REDIS", "REDIS_OBJECT", "REDIS_PUBSUB").
// Ordre de détection automatique :
//  1. <PREFIX>_CLUSTER_ADDRS -> Redis Cluster
//  2. <PREFIX>_SENTINEL_ADDRS -> Redis Sentinel HA
//  3. <PREFIX>_ADDR -> Redis Standalone
//  4. Tous vides pour REDIS_OBJECT / REDIS_PUBSUB -> IsConfigured() == false (collapse sur RdbCore sans connexion supplémentaire)
func ResolveTierConfig(prefix string) RedisTierConfig {
	prefix = strings.ToUpper(strings.TrimSpace(prefix))
	if prefix == "" {
		prefix = "REDIS"
	}

	clusterAddrs := parseCommaAddrs(os.Getenv(prefix + "_CLUSTER_ADDRS"))
	sentinelAddrs := parseCommaAddrs(os.Getenv(prefix + "_SENTINEL_ADDRS"))
	addr := strings.TrimSpace(os.Getenv(prefix + "_ADDR"))
	if len(clusterAddrs) == 0 && strings.Contains(addr, ",") {
		clusterAddrs = parseCommaAddrs(addr)
		addr = ""
	}

	if prefix == "REDIS" && len(clusterAddrs) == 0 && len(sentinelAddrs) == 0 && addr == "" {
		addr = defaultRedisAddr
	}

	masterName := strings.TrimSpace(os.Getenv(prefix + "_MASTER_NAME"))
	if masterName == "" {
		masterName = defaultRedisMasterName
	}

	tlsFallbackHost := addr
	if len(clusterAddrs) > 0 {
		tlsFallbackHost = clusterAddrs[0]
	} else if len(sentinelAddrs) > 0 {
		tlsFallbackHost = sentinelAddrs[0]
	}

	password := os.Getenv(prefix + "_PASSWORD")
	if password == "" && prefix != "REDIS" {
		password = os.Getenv("REDIS_PASSWORD")
	}
	sentinelPassword := os.Getenv(prefix + "_SENTINEL_PASSWORD")
	if sentinelPassword == "" && prefix != "REDIS" {
		sentinelPassword = os.Getenv("REDIS_SENTINEL_PASSWORD")
	}

	return RedisTierConfig{
		Prefix:           prefix,
		Addr:             addr,
		ClusterAddrs:     clusterAddrs,
		MasterName:       masterName,
		SentinelAddrs:    sentinelAddrs,
		Password:         password,
		SentinelPassword: sentinelPassword,
		DB:               0,
		TLSConfig:        resolveTierTLS(prefix, tlsFallbackHost),
		MaxRetries:       parsePositiveIntEnv(prefix+"_MAX_RETRIES", parsePositiveIntEnv("REDIS_MAX_RETRIES", defaultRedisMaxRetries)),
		DialTimeout:      parsePositiveSecondsEnv(prefix+"_DIAL_TIMEOUT_SEC", parsePositiveSecondsEnv("REDIS_DIAL_TIMEOUT_SEC", defaultRedisDialTimeout)),
		ReadTimeout:      parsePositiveSecondsEnv(prefix+"_READ_TIMEOUT_SEC", parsePositiveSecondsEnv("REDIS_READ_TIMEOUT_SEC", defaultRedisReadTimeout)),
		WriteTimeout:     parsePositiveSecondsEnv(prefix+"_WRITE_TIMEOUT_SEC", parsePositiveSecondsEnv("REDIS_WRITE_TIMEOUT_SEC", defaultRedisWriteTimeout)),
		PoolSize:         parsePositiveIntEnv(prefix+"_POOL_SIZE", parsePositiveIntEnv("REDIS_POOL_SIZE", defaultRedisPoolSize)),
		RouteByLatency:   strings.EqualFold(envWithFallback(prefix+"_ROUTE_BY_LATENCY", "REDIS_ROUTE_BY_LATENCY"), "true"),
		RouteRandomly:    strings.EqualFold(envWithFallback(prefix+"_ROUTE_RANDOMLY", "REDIS_ROUTE_RANDOMLY"), "true"),
	}
}

// ResolveRedisConfig résout la configuration complète du tier Core (REDIS_*).
func ResolveRedisConfig() RedisConfig {
	return ResolveTierConfig("REDIS")
}

// StandaloneOptions convertit la configuration en *redis.Options pour un déploiement mono-instance.
func (c RedisTierConfig) StandaloneOptions() *redis.Options {
	return &redis.Options{
		Addr:         c.Addr,
		Password:     c.Password,
		DB:           c.DB,
		TLSConfig:    c.TLSConfig,
		MaxRetries:   c.MaxRetries,
		DialTimeout:  c.DialTimeout,
		ReadTimeout:  c.ReadTimeout,
		WriteTimeout: c.WriteTimeout,
		PoolSize:     c.PoolSize,
	}
}

// FailoverOptions convertit la configuration en *redis.FailoverOptions pour un cluster Redis Sentinel HA.
func (c RedisTierConfig) FailoverOptions() *redis.FailoverOptions {
	return &redis.FailoverOptions{
		MasterName:       c.MasterName,
		SentinelAddrs:    c.SentinelAddrs,
		Password:         c.Password,
		SentinelPassword: c.SentinelPassword,
		DB:               c.DB,
		TLSConfig:        c.TLSConfig,
		MaxRetries:       c.MaxRetries,
		DialTimeout:      c.DialTimeout,
		ReadTimeout:      c.ReadTimeout,
		WriteTimeout:     c.WriteTimeout,
		PoolSize:         c.PoolSize,
		RouteByLatency:   c.RouteByLatency,
		RouteRandomly:    c.RouteRandomly,
	}
}

// ClusterOptions convertit la configuration en *redis.ClusterOptions pour un Redis Cluster sharded.
func (c RedisTierConfig) ClusterOptions() *redis.ClusterOptions {
	return &redis.ClusterOptions{
		Addrs:          c.ClusterAddrs,
		Password:       c.Password,
		TLSConfig:      c.TLSConfig,
		MaxRetries:     c.MaxRetries,
		DialTimeout:    c.DialTimeout,
		ReadTimeout:    c.ReadTimeout,
		WriteTimeout:   c.WriteTimeout,
		PoolSize:       c.PoolSize,
		RouteByLatency: c.RouteByLatency,
		RouteRandomly:  c.RouteRandomly,
	}
}

// ResolveRedisOptions construit les options Redis Standalone (conservé pour rétrocompatibilité).
func ResolveRedisOptions() *redis.Options {
	return ResolveRedisConfig().StandaloneOptions()
}

// NewRedisClient instancie un redis.UniversalClient adapté au mode détecté (Cluster, Sentinel ou Standalone).
func NewRedisClient(cfg RedisTierConfig) redis.UniversalClient {
	if cfg.IsCluster() {
		return redis.NewClusterClient(cfg.ClusterOptions())
	}
	if cfg.IsSentinel() {
		return redis.NewFailoverClient(cfg.FailoverOptions())
	}
	return redis.NewClient(cfg.StandaloneOptions())
}

// InitRedis initialise les 3 tiers logiques Redis (Core, Object Pool, Pub/Sub).
// Lorsque REDIS_OBJECT_* ou REDIS_PUBSUB_* sont absents, ils pointent vers RdbCore sans ouvrir de pool TCP supplémentaire.
func InitRedis() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Tier 1 : Redis Core (Critique — échec fatal au démarrage)
	coreCfg := ResolveTierConfig("REDIS")
	coreClient := NewRedisClient(coreCfg)
	if err := coreClient.Ping(ctx).Err(); err != nil {
		numan_log.Fatal(ctx).Err(err).
			Str("redis_tier", "core").
			Str("redis_mode", coreCfg.Mode()).
			Str("redis_addr", coreCfg.Addr).
			Int("redis_cluster_nodes", len(coreCfg.ClusterAddrs)).
			Int("redis_sentinel_count", len(coreCfg.SentinelAddrs)).
			Bool("redis_tls", coreCfg.TLSConfig != nil).
			Msg("Impossible de se connecter à Redis Core (Tier 1)")
	}
	RdbCore = coreClient
	Rdb = coreClient

	numan_log.Info(ctx).
		Str("redis_tier", "core").
		Str("redis_mode", coreCfg.Mode()).
		Str("redis_addr", coreCfg.Addr).
		Int("redis_cluster_nodes", len(coreCfg.ClusterAddrs)).
		Int("redis_sentinel_count", len(coreCfg.SentinelAddrs)).
		Bool("redis_tls", coreCfg.TLSConfig != nil).
		Int("pool_size", coreCfg.PoolSize).
		Msg("Connexion à Redis Core (Tier 1) réussie")

	// Tier 2 : Redis Object Pool (Optionnel — tolère le démarrage dégradé si MongoDB L2 est actif)
	objCfg := ResolveTierConfig("REDIS_OBJECT")
	if objCfg.IsConfigured() {
		objClient := NewRedisClient(objCfg)
		if err := objClient.Ping(ctx).Err(); err != nil {
			numan_log.Warn(ctx).Err(err).
				Str("redis_tier", "objects").
				Str("redis_mode", objCfg.Mode()).
				Msg("Redis Object Pool (Tier 2) injoignable au démarrage ; mode dégradé toléré (repli MongoDB L2)")
		} else {
			numan_log.Info(ctx).
				Str("redis_tier", "objects").
				Str("redis_mode", objCfg.Mode()).
				Int("redis_cluster_nodes", len(objCfg.ClusterAddrs)).
				Int("pool_size", objCfg.PoolSize).
				Msg("Connexion à Redis Object Pool (Tier 2 — 100% RAM éphémère) réussie")
		}
		RdbObjects = objClient
	} else {
		RdbObjects = RdbCore
		numan_log.Info(ctx).Str("redis_tier", "objects").Msg("Redis Object Pool non configuré : repli transparent sur Redis Core (0 connexion supplémentaire)")
	}

	// Tier 3 : Redis Pub/Sub (Optionnel — repli sur Redis Core si non configuré)
	pubsubCfg := ResolveTierConfig("REDIS_PUBSUB")
	if pubsubCfg.IsConfigured() {
		psClient := NewRedisClient(pubsubCfg)
		if err := psClient.Ping(ctx).Err(); err != nil {
			numan_log.Warn(ctx).Err(err).
				Str("redis_tier", "pubsub").
				Str("redis_mode", pubsubCfg.Mode()).
				Msg("Redis Pub/Sub (Tier 3) injoignable au démarrage")
		} else {
			numan_log.Info(ctx).
				Str("redis_tier", "pubsub").
				Str("redis_mode", pubsubCfg.Mode()).
				Int("redis_cluster_nodes", len(pubsubCfg.ClusterAddrs)).
				Bool("sharded_pubsub", IsClusterClient(psClient)).
				Msg("Connexion à Redis Pub/Sub (Tier 3) réussie")
		}
		RdbPubSub = psClient
	} else {
		RdbPubSub = RdbCore
		numan_log.Info(ctx).Str("redis_tier", "pubsub").Msg("Redis Pub/Sub non configuré : repli transparent sur Redis Core (0 connexion supplémentaire)")
	}
}

// ShardMemorySnapshot capture l'état mémoire et client d'un nœud/shard Redis.
type ShardMemorySnapshot struct {
	ShardID          string  `json:"shard_id"`
	Role             string  `json:"role"`
	NodeHostname     string  `json:"node_hostname"`
	UsedBytes        int64   `json:"used_bytes"`
	MaxBytes         int64   `json:"max_bytes"`
	FillRatio        float64 `json:"fill_ratio"`
	EvictedKeys      int64   `json:"evicted_keys"`
	ConnectedClients int64   `json:"connected_clients"`
}

// TierTelemetrySnapshot agrège les statistiques temps réel d'un tier Redis.
type TierTelemetrySnapshot struct {
	Tier             string                `json:"tier"`
	Mode             string                `json:"mode"`
	Dedicated        bool                  `json:"dedicated"`
	Shards           []ShardMemorySnapshot `json:"shards"`
	TotalUsedBytes   int64                 `json:"total_used_bytes"`
	TotalMaxBytes    int64                 `json:"total_max_bytes"`
	TotalEvictedKeys int64                 `json:"total_evicted_keys"`
	PubSubChannels   int64                 `json:"pubsub_channels,omitempty"`
}

// ParseEvictedKeys extrait la métrique `evicted_keys` de la sortie Redis `INFO stats`.
func ParseEvictedKeys(infoStats string) (int64, bool) {
	return parseInfoInt64(infoStats, "evicted_keys:")
}

func parseInfoInt64(info, prefix string) (int64, bool) {
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			rawVal := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if v, err := strconv.ParseInt(rawVal, 10, 64); err == nil && v >= 0 {
				return v, true
			}
		}
	}
	return 0, false
}

// CollectEvictedKeys interroge Redis Core `INFO stats` pour exposer le compteur d'évictions volatile-lfu (surveillance Tombstones).
func CollectEvictedKeys(ctx context.Context) (int64, bool) {
	return CollectTierEvictedKeys(ctx, CoreClient())
}

// CollectTierEvictedKeys somme `evicted_keys` sur un client Redis (Standalone, Sentinel ou tous les masters d'un Cluster).
func CollectTierEvictedKeys(ctx context.Context, client redis.UniversalClient) (int64, bool) {
	if client == nil {
		return 0, false
	}
	if clusterClient, ok := client.(*redis.ClusterClient); ok {
		var total int64
		var found bool
		_ = clusterClient.ForEachMaster(ctx, func(c context.Context, master *redis.Client) error {
			info, err := master.Info(c, "stats").Result()
			if err != nil {
				return nil
			}
			if v, ok := ParseEvictedKeys(info); ok {
				total += v
				found = true
			}
			return nil
		})
		return total, found
	}
	info, err := client.Info(ctx, "stats").Result()
	if err != nil {
		return 0, false
	}
	return ParseEvictedKeys(info)
}

func collectSingleNodeMemory(ctx context.Context, shardID, role, host string, client *redis.Client) ShardMemorySnapshot {
	snap := ShardMemorySnapshot{
		ShardID:      shardID,
		Role:         role,
		NodeHostname: host,
	}
	if infoMem, err := client.Info(ctx, "memory").Result(); err == nil {
		if used, ok := parseInfoInt64(infoMem, "used_memory:"); ok {
			snap.UsedBytes = used
		}
		if maxMem, ok := parseInfoInt64(infoMem, "maxmemory:"); ok {
			snap.MaxBytes = maxMem
		}
		if snap.MaxBytes > 0 {
			snap.FillRatio = float64(snap.UsedBytes) / float64(snap.MaxBytes)
		}
	}
	if infoStats, err := client.Info(ctx, "stats").Result(); err == nil {
		if evicted, ok := ParseEvictedKeys(infoStats); ok {
			snap.EvictedKeys = evicted
		}
	}
	if infoClients, err := client.Info(ctx, "clients").Result(); err == nil {
		if clients, ok := parseInfoInt64(infoClients, "connected_clients:"); ok {
			snap.ConnectedClients = clients
		}
	}
	return snap
}

// CollectTierTelemetry collecte la télémétrie mémoire/évictions d'un tier Redis (tous les masters en mode Cluster).
func CollectTierTelemetry(ctx context.Context, tierName string, client redis.UniversalClient, dedicated bool) TierTelemetrySnapshot {
	snap := TierTelemetrySnapshot{
		Tier:      tierName,
		Mode:      TierTopologyMode(client),
		Dedicated: dedicated,
		Shards:    make([]ShardMemorySnapshot, 0, 1),
	}
	if client == nil {
		return snap
	}

	if clusterClient, ok := client.(*redis.ClusterClient); ok {
		idx := 0
		_ = clusterClient.ForEachMaster(ctx, func(c context.Context, master *redis.Client) error {
			idx++
			addr := ""
			if opts := master.Options(); opts != nil {
				addr = opts.Addr
			}
			sh := collectSingleNodeMemory(c, "master-"+strconv.Itoa(idx), "master", addr, master)
			snap.Shards = append(snap.Shards, sh)
			snap.TotalUsedBytes += sh.UsedBytes
			snap.TotalMaxBytes += sh.MaxBytes
			snap.TotalEvictedKeys += sh.EvictedKeys
			return nil
		})
	} else if stdClient, ok := client.(*redis.Client); ok {
		addr := ""
		if opts := stdClient.Options(); opts != nil {
			addr = opts.Addr
		}
		sh := collectSingleNodeMemory(ctx, "primary-1", "master", addr, stdClient)
		snap.Shards = append(snap.Shards, sh)
		snap.TotalUsedBytes = sh.UsedBytes
		snap.TotalMaxBytes = sh.MaxBytes
		snap.TotalEvictedKeys = sh.EvictedKeys
	}

	if tierName == "pubsub" {
		if chs, err := client.PubSubChannels(ctx, "*").Result(); err == nil {
			snap.PubSubChannels = int64(len(chs))
		}
	}

	return snap
}

// GetWithTimeout récupère une valeur avec un timeout custom sur Redis Core.
func GetWithTimeout(key string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return CoreClient().Get(ctx, key).Result()
}

// SetWithTimeout ajoute une valeur avec un timeout custom sur Redis Core.
func SetWithTimeout(key string, value interface{}, expiration time.Duration, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return CoreClient().Set(ctx, key, value, expiration).Err()
}

// CloseRedis ferme proprement tous les clients Redis distincts lors d'un arrêt gracieux.
func CloseRedis() error {
	seen := make(map[redis.UniversalClient]struct{}, 3)
	var firstErr error
	for _, c := range []redis.UniversalClient{RdbCore, Rdb, RdbObjects, RdbPubSub} {
		if c == nil {
			continue
		}
		if _, already := seen[c]; already {
			continue
		}
		seen[c] = struct{}{}
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
