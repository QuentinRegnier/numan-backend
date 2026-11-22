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
	"github.com/go-redis/redis/v8"
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

// Rdb est le client Redis global (Standalone ou Sentinel FailoverClient, 100 % compatible *redis.Client).
// NOTE CRITIQUE COHÉRENCE : Rdb cible le Master par défaut (pas de ReplicaOnly) afin de garantir
// une cohérence stricte pour les Tombstones, les files Write-Behind (EnqueueDB), les verrous distribués
// (AcquireCronLock) et le ratchet HMAC des sessions.
var Rdb *redis.Client

// RedisConfig regroupe la configuration résolue pour Redis (mode Standalone ou Sentinel HA).
type RedisConfig struct {
	Addr             string
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
}

// IsSentinel indique si le mode Haute Disponibilité Redis Sentinel est actif.
func (c RedisConfig) IsSentinel() bool {
	return len(c.SentinelAddrs) > 0
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

func parseSentinelAddrs(raw string) []string {
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

func resolveRedisTLS(fallbackHostAddr string) *tls.Config {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("REDIS_TLS")), "true") {
		return nil
	}
	serverName := strings.TrimSpace(os.Getenv("REDIS_TLS_SERVER_NAME"))
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

// ResolveRedisConfig résout la configuration complète Redis (détection automatique Standalone vs Sentinel HA).
func ResolveRedisConfig() RedisConfig {
	addr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if addr == "" {
		addr = defaultRedisAddr
	}
	masterName := strings.TrimSpace(os.Getenv("REDIS_MASTER_NAME"))
	if masterName == "" {
		masterName = defaultRedisMasterName
	}
	sentinelAddrs := parseSentinelAddrs(os.Getenv("REDIS_SENTINEL_ADDRS"))

	tlsFallbackHost := addr
	if len(sentinelAddrs) > 0 {
		tlsFallbackHost = sentinelAddrs[0]
	}

	return RedisConfig{
		Addr:             addr,
		MasterName:       masterName,
		SentinelAddrs:    sentinelAddrs,
		Password:         os.Getenv("REDIS_PASSWORD"),
		SentinelPassword: os.Getenv("REDIS_SENTINEL_PASSWORD"),
		DB:               0,
		TLSConfig:        resolveRedisTLS(tlsFallbackHost),
		MaxRetries:       parsePositiveIntEnv("REDIS_MAX_RETRIES", defaultRedisMaxRetries),
		DialTimeout:      parsePositiveSecondsEnv("REDIS_DIAL_TIMEOUT_SEC", defaultRedisDialTimeout),
		ReadTimeout:      parsePositiveSecondsEnv("REDIS_READ_TIMEOUT_SEC", defaultRedisReadTimeout),
		WriteTimeout:     parsePositiveSecondsEnv("REDIS_WRITE_TIMEOUT_SEC", defaultRedisWriteTimeout),
		PoolSize:         parsePositiveIntEnv("REDIS_POOL_SIZE", defaultRedisPoolSize),
		RouteByLatency:   strings.EqualFold(strings.TrimSpace(os.Getenv("REDIS_ROUTE_BY_LATENCY")), "true"),
	}
}

// StandaloneOptions convertit la configuration en *redis.Options pour un déploiement mono-instance.
func (c RedisConfig) StandaloneOptions() *redis.Options {
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
func (c RedisConfig) FailoverOptions() *redis.FailoverOptions {
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
	}
}

// ResolveRedisOptions construit les options Redis Standalone (conservé pour rétrocompatibilité).
func ResolveRedisOptions() *redis.Options {
	return ResolveRedisConfig().StandaloneOptions()
}

// NewRedisClient instancie un *redis.Client adapté au mode détecté (Sentinel FailoverClient ou Standalone).
func NewRedisClient(cfg RedisConfig) *redis.Client {
	if cfg.IsSentinel() {
		return redis.NewFailoverClient(cfg.FailoverOptions())
	}
	return redis.NewClient(cfg.StandaloneOptions())
}

// InitRedis initialise la connexion Redis (Standalone ou Sentinel HA) avec timeout et journalisation structurée.
func InitRedis() {
	cfg := ResolveRedisConfig()
	Rdb = NewRedisClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := Rdb.Ping(ctx).Err(); err != nil {
		if cfg.IsSentinel() {
			numan_log.Fatal(ctx).Err(err).
				Str("redis_mode", "sentinel").
				Str("redis_master_name", cfg.MasterName).
				Int("redis_sentinel_count", len(cfg.SentinelAddrs)).
				Bool("redis_tls", cfg.TLSConfig != nil).
				Msg("Impossible de se connecter à Redis via Sentinel")
		}
		numan_log.Fatal(ctx).Err(err).
			Str("redis_mode", "standalone").
			Str("redis_addr", cfg.Addr).
			Bool("redis_tls", cfg.TLSConfig != nil).
			Msg("Impossible de se connecter à Redis")
	}

	if cfg.IsSentinel() {
		numan_log.Info(ctx).
			Str("redis_mode", "sentinel").
			Str("redis_master_name", cfg.MasterName).
			Int("redis_sentinel_count", len(cfg.SentinelAddrs)).
			Bool("redis_tls", cfg.TLSConfig != nil).
			Int("pool_size", cfg.PoolSize).
			Int("max_retries", cfg.MaxRetries).
			Msg("Connexion à Redis Sentinel HA réussie (bascule automatique active)")
		return
	}

	numan_log.Info(ctx).
		Str("redis_mode", "standalone").
		Str("redis_addr", cfg.Addr).
		Bool("redis_tls", cfg.TLSConfig != nil).
		Int("pool_size", cfg.PoolSize).
		Int("max_retries", cfg.MaxRetries).
		Msg("Connexion à Redis réussie")
}

// ParseEvictedKeys extrait la métrique `evicted_keys` de la sortie Redis `INFO stats`.
func ParseEvictedKeys(infoStats string) (int64, bool) {
	for _, line := range strings.Split(infoStats, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "evicted_keys:") {
			rawVal := strings.TrimSpace(strings.TrimPrefix(line, "evicted_keys:"))
			if v, err := strconv.ParseInt(rawVal, 10, 64); err == nil && v >= 0 {
				return v, true
			}
		}
	}
	return 0, false
}

// CollectEvictedKeys interroge Redis `INFO stats` pour exposer le compteur d'évictions volatile-lfu (surveillance Tombstones).
func CollectEvictedKeys(ctx context.Context) (int64, bool) {
	if Rdb == nil {
		return 0, false
	}
	info, err := Rdb.Info(ctx, "stats").Result()
	if err != nil {
		return 0, false
	}
	return ParseEvictedKeys(info)
}

// GetWithTimeout récupère une valeur avec un timeout custom
func GetWithTimeout(key string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return Rdb.Get(ctx, key).Result()
}

// SetWithTimeout ajoute une valeur avec un timeout custom
func SetWithTimeout(key string, value interface{}, expiration time.Duration, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return Rdb.Set(ctx, key, value, expiration).Err()
}

// CloseRedis ferme proprement le client Redis lors d'un arrêt gracieux.
func CloseRedis() error {
	if Rdb == nil {
		return nil
	}
	return Rdb.Close()
}
