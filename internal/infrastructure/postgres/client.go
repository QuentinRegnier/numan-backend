package postgres

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
)

const (
	defaultPostgresHost            = "postgres"
	defaultPostgresPort            = "5432"
	defaultPostgresSSLMode         = "disable"
	defaultPostgresMaxOpenConns    = 15
	defaultPostgresMaxIdleConns    = 10
	defaultPostgresConnMaxLifetime = 15 * time.Minute
	defaultPostgresConnMaxIdleTime = 5 * time.Minute
)

var (
	// PostgresDB est le pool PostgreSQL Primary (Read/Write). Conservé tel quel pour 100 % de rétrocompatibilité.
	PostgresDB *sql.DB
	// PostgresReadDB est le pool PostgreSQL Read Replica (lecture seule). Pointe vers PostgresDB en mode standalone ou repli.
	PostgresReadDB *sql.DB
)

// WriteDB retourne le pool PostgreSQL Primary (écritures, transactions et lectures critiques sans lag).
func WriteDB() *sql.DB {
	return PostgresDB
}

// ReadDB retourne le pool PostgreSQL dédié aux Read Replicas, avec repli automatique transparent sur le Primary.
func ReadDB() *sql.DB {
	if PostgresReadDB != nil {
		return PostgresReadDB
	}
	return PostgresDB
}

// HasDedicatedReadPool indique si un pool Read Replica distinct du Primary est actuellement actif.
func HasDedicatedReadPool() bool {
	return PostgresReadDB != nil && PostgresReadDB != PostgresDB
}

// PostgresConfig regroupe les paramètres de connexion et de pool PostgreSQL résolus depuis l'environnement.
type PostgresConfig struct {
	DSN             string
	Host            string
	Port            string
	DBName          string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
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

func parsePositiveDurationSecondsEnv(key string, fallback time.Duration) time.Duration {
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

func buildPostgresDSN(host, port, user, password, dbname, sslMode, sslRootCert string) string {
	q := url.Values{}
	q.Set("sslmode", sslMode)
	if sslRootCert != "" {
		q.Set("sslrootcert", sslRootCert)
	}
	var userInfo *url.Userinfo
	if user != "" {
		userInfo = url.UserPassword(user, password)
	}
	u := &url.URL{
		Scheme:   "postgres",
		User:     userInfo,
		Host:     net.JoinHostPort(host, port),
		Path:     "/" + dbname,
		RawQuery: q.Encode(),
	}
	return u.String()
}

// ResolvePostgresConfig résout la configuration PostgreSQL Primary (DSN + bornes du pool) depuis les variables d'environnement.
func ResolvePostgresConfig() PostgresConfig {
	host := strings.TrimSpace(os.Getenv("POSTGRES_HOST"))
	if host == "" {
		host = defaultPostgresHost
	}
	port := strings.TrimSpace(os.Getenv("POSTGRES_PORT"))
	if port == "" {
		port = defaultPostgresPort
	}
	user := strings.TrimSpace(os.Getenv("POSTGRES_USER"))
	password := os.Getenv("POSTGRES_PASSWORD")
	dbname := strings.TrimSpace(os.Getenv("POSTGRES_DB"))
	sslMode := strings.TrimSpace(os.Getenv("POSTGRES_SSLMODE"))
	if sslMode == "" {
		sslMode = defaultPostgresSSLMode
	}
	sslRootCert := strings.TrimSpace(os.Getenv("POSTGRES_SSLROOTCERT"))

	dsn := strings.TrimSpace(os.Getenv("DB_DSN"))
	if dsn == "" {
		dsn = buildPostgresDSN(host, port, user, password, dbname, sslMode, sslRootCert)
	}

	maxOpen := parsePositiveIntEnv("POSTGRES_MAX_OPEN_CONNS", defaultPostgresMaxOpenConns)
	maxIdle := parsePositiveIntEnv("POSTGRES_MAX_IDLE_CONNS", defaultPostgresMaxIdleConns)
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}

	return PostgresConfig{
		DSN:             dsn,
		Host:            host,
		Port:            port,
		DBName:          dbname,
		SSLMode:         sslMode,
		MaxOpenConns:    maxOpen,
		MaxIdleConns:    maxIdle,
		ConnMaxLifetime: parsePositiveDurationSecondsEnv("POSTGRES_CONN_MAX_LIFETIME_SEC", defaultPostgresConnMaxLifetime),
		ConnMaxIdleTime: parsePositiveDurationSecondsEnv("POSTGRES_CONN_MAX_IDLE_TIME_SEC", defaultPostgresConnMaxIdleTime),
	}
}

// ResolvePostgresReadConfig résout la configuration du pool PostgreSQL Read Replica (POSTGRES_READ_HOST / DB_READ_DSN).
// Retourne enabled=false si aucune cible Read Replica n'est configurée (mode all-in-one / dev local).
func ResolvePostgresReadConfig() (PostgresConfig, bool) {
	readDSN := strings.TrimSpace(os.Getenv("DB_READ_DSN"))
	readHost := strings.TrimSpace(os.Getenv("POSTGRES_READ_HOST"))
	if readDSN == "" && readHost == "" {
		return PostgresConfig{}, false
	}

	primaryCfg := ResolvePostgresConfig()

	readPort := strings.TrimSpace(os.Getenv("POSTGRES_READ_PORT"))
	if readPort == "" {
		readPort = primaryCfg.Port
	}
	user := strings.TrimSpace(os.Getenv("POSTGRES_USER"))
	password := os.Getenv("POSTGRES_PASSWORD")
	sslRootCert := strings.TrimSpace(os.Getenv("POSTGRES_SSLROOTCERT"))

	if readDSN == "" {
		readDSN = buildPostgresDSN(readHost, readPort, user, password, primaryCfg.DBName, primaryCfg.SSLMode, sslRootCert)
	}

	maxOpen := parsePositiveIntEnv("POSTGRES_READ_MAX_OPEN_CONNS", primaryCfg.MaxOpenConns)
	maxIdle := parsePositiveIntEnv("POSTGRES_READ_MAX_IDLE_CONNS", primaryCfg.MaxIdleConns)
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}

	return PostgresConfig{
		DSN:             readDSN,
		Host:            readHost,
		Port:            readPort,
		DBName:          primaryCfg.DBName,
		SSLMode:         primaryCfg.SSLMode,
		MaxOpenConns:    maxOpen,
		MaxIdleConns:    maxIdle,
		ConnMaxLifetime: primaryCfg.ConnMaxLifetime,
		ConnMaxIdleTime: primaryCfg.ConnMaxIdleTime,
	}, true
}

// ApplyPoolConfig applique les bornes de pool recommandées pour Kubernetes/HPA sur un *sql.DB.
func ApplyPoolConfig(db *sql.DB, cfg PostgresConfig) {
	if db == nil {
		return
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
}

func InitPostgres() {
	ctx := context.Background()
	cfg := ResolvePostgresConfig()

	var err error
	PostgresDB, err = sql.Open("postgres", cfg.DSN)
	if err != nil {
		numan_log.Fatal(ctx).Err(err).
			Str("postgres_role", "primary").
			Str("postgres_host", cfg.Host).
			Str("postgres_port", cfg.Port).
			Str("postgres_db", cfg.DBName).
			Msg("Erreur connexion PostgreSQL Primary")
	}

	ApplyPoolConfig(PostgresDB, cfg)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := PostgresDB.PingContext(pingCtx); err != nil {
		numan_log.Fatal(ctx).Err(err).
			Str("postgres_role", "primary").
			Str("postgres_host", cfg.Host).
			Str("postgres_port", cfg.Port).
			Str("postgres_db", cfg.DBName).
			Msg("Ping PostgreSQL Primary échoué")
	}

	numan_log.Info(ctx).
		Str("postgres_role", "primary").
		Str("postgres_host", cfg.Host).
		Str("postgres_port", cfg.Port).
		Str("postgres_db", cfg.DBName).
		Str("postgres_sslmode", cfg.SSLMode).
		Int("max_open_conns", cfg.MaxOpenConns).
		Int("max_idle_conns", cfg.MaxIdleConns).
		Str("conn_max_lifetime", cfg.ConnMaxLifetime.String()).
		Str("conn_max_idle_time", cfg.ConnMaxIdleTime.String()).
		Msg("Connecté à PostgreSQL Primary avec succès (pool borné)")

	// Initialisation du pool Read Replica (optionnel, tolérant aux pannes au démarrage)
	readCfg, hasReadReplica := ResolvePostgresReadConfig()
	if !hasReadReplica {
		PostgresReadDB = PostgresDB
		numan_log.Info(ctx).
			Str("postgres_host", cfg.Host).
			Msg("PostgreSQL Read Replica non configuré : ReadDB partage le pool Primary (mode standalone)")
		return
	}

	readDB, errOpen := sql.Open("postgres", readCfg.DSN)
	if errOpen != nil {
		numan_log.Warn(ctx).Err(errOpen).
			Str("postgres_role", "read_replica").
			Str("postgres_read_host", readCfg.Host).
			Str("postgres_read_port", readCfg.Port).
			Msg("Échec d'ouverture du pool PostgreSQL Read Replica : repli automatique sur le Primary")
		PostgresReadDB = PostgresDB
		return
	}

	ApplyPoolConfig(readDB, readCfg)

	readPingCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readCancel()

	if errPing := readDB.PingContext(readPingCtx); errPing != nil {
		_ = readDB.Close()
		numan_log.Warn(ctx).Err(errPing).
			Str("postgres_role", "read_replica").
			Str("postgres_read_host", readCfg.Host).
			Str("postgres_read_port", readCfg.Port).
			Msg("Ping PostgreSQL Read Replica échoué au démarrage : repli automatique sur le Primary")
		PostgresReadDB = PostgresDB
		return
	}

	PostgresReadDB = readDB
	numan_log.Info(ctx).
		Str("postgres_role", "read_replica").
		Str("postgres_read_host", readCfg.Host).
		Str("postgres_read_port", readCfg.Port).
		Str("postgres_db", readCfg.DBName).
		Str("postgres_sslmode", readCfg.SSLMode).
		Int("max_open_conns", readCfg.MaxOpenConns).
		Int("max_idle_conns", readCfg.MaxIdleConns).
		Msg("Connecté à PostgreSQL Read Replica avec succès (Read/Write Split actif)")
}

// ClosePostgres ferme proprement les pools PostgreSQL (Read Replica + Primary) lors d'un arrêt gracieux.
func ClosePostgres() error {
	var errRead, errWrite error
	if PostgresReadDB != nil && PostgresReadDB != PostgresDB {
		errRead = PostgresReadDB.Close()
	}
	if PostgresDB != nil {
		errWrite = PostgresDB.Close()
	}
	return errors.Join(errRead, errWrite)
}
