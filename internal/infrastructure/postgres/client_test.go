package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearPostgresEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"DB_DSN", "DB_READ_DSN",
		"POSTGRES_HOST", "POSTGRES_PORT", "POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB",
		"POSTGRES_READ_HOST", "POSTGRES_READ_PORT",
		"POSTGRES_SSLMODE", "POSTGRES_SSLROOTCERT",
		"POSTGRES_MAX_OPEN_CONNS", "POSTGRES_MAX_IDLE_CONNS",
		"POSTGRES_READ_MAX_OPEN_CONNS", "POSTGRES_READ_MAX_IDLE_CONNS",
		"POSTGRES_CONN_MAX_LIFETIME_SEC", "POSTGRES_CONN_MAX_IDLE_TIME_SEC",
	} {
		t.Setenv(key, "")
	}
}

func TestResolvePostgresConfig_DefaultsAndPoolBounds(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("POSTGRES_USER", "numan")
	t.Setenv("POSTGRES_PASSWORD", "p@ss#word")
	t.Setenv("POSTGRES_DB", "numan")

	cfg := ResolvePostgresConfig()
	assert.Equal(t, "postgres", cfg.Host)
	assert.Equal(t, "5432", cfg.Port)
	assert.Equal(t, "disable", cfg.SSLMode)
	assert.Equal(t, 15, cfg.MaxOpenConns)
	assert.Equal(t, 10, cfg.MaxIdleConns)
	assert.Equal(t, 15*time.Minute, cfg.ConnMaxLifetime)
	assert.Equal(t, 5*time.Minute, cfg.ConnMaxIdleTime)
	assert.Equal(t, "postgres://numan:p%40ss%23word@postgres:5432/numan?sslmode=disable", cfg.DSN)
}

func TestResolvePostgresConfig_CustomPoolClampAndTLS(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("POSTGRES_HOST", "pg-primary.internal")
	t.Setenv("POSTGRES_PORT", "6432")
	t.Setenv("POSTGRES_USER", "app")
	t.Setenv("POSTGRES_PASSWORD", "secret")
	t.Setenv("POSTGRES_DB", "numan")
	t.Setenv("POSTGRES_SSLMODE", "verify-full")
	t.Setenv("POSTGRES_SSLROOTCERT", "/etc/ssl/certs/pg-ca.crt")
	t.Setenv("POSTGRES_MAX_OPEN_CONNS", "8")
	t.Setenv("POSTGRES_MAX_IDLE_CONNS", "25") // doit être clampé à MaxOpenConns
	t.Setenv("POSTGRES_CONN_MAX_LIFETIME_SEC", "600")
	t.Setenv("POSTGRES_CONN_MAX_IDLE_TIME_SEC", "120")

	cfg := ResolvePostgresConfig()
	assert.Equal(t, 8, cfg.MaxOpenConns)
	assert.Equal(t, 8, cfg.MaxIdleConns)
	assert.Equal(t, 600*time.Second, cfg.ConnMaxLifetime)
	assert.Equal(t, 120*time.Second, cfg.ConnMaxIdleTime)
	assert.Contains(t, cfg.DSN, "postgres://app:secret@pg-primary.internal:6432/numan?")
	assert.Contains(t, cfg.DSN, "sslmode=verify-full")
	assert.Contains(t, cfg.DSN, "sslrootcert=%2Fetc%2Fssl%2Fcerts%2Fpg-ca.crt")
}

func TestResolvePostgresConfig_ExplicitDSNAndInvalidPoolValuesFallback(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("DB_DSN", "host=postgres user=numan password=secret dbname=numan port=5432 sslmode=require")
	t.Setenv("POSTGRES_MAX_OPEN_CONNS", "-5")
	t.Setenv("POSTGRES_MAX_IDLE_CONNS", "not-a-number")

	cfg := ResolvePostgresConfig()
	assert.Equal(t, "host=postgres user=numan password=secret dbname=numan port=5432 sslmode=require", cfg.DSN)
	assert.Equal(t, 15, cfg.MaxOpenConns)
	assert.Equal(t, 10, cfg.MaxIdleConns)
}

func TestResolvePostgresReadConfig_StandaloneDisabledWhenEmpty(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("POSTGRES_HOST", "postgres")
	t.Setenv("POSTGRES_USER", "numan")
	t.Setenv("POSTGRES_PASSWORD", "secret")
	t.Setenv("POSTGRES_DB", "numan")

	cfg, enabled := ResolvePostgresReadConfig()
	assert.False(t, enabled)
	assert.Empty(t, cfg.DSN)
}

func TestResolvePostgresReadConfig_HAReadReplicaHostAndPoolLimits(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("POSTGRES_HOST", "postgres-rw")
	t.Setenv("POSTGRES_READ_HOST", "postgres-ro")
	t.Setenv("POSTGRES_READ_PORT", "5433")
	t.Setenv("POSTGRES_USER", "numan")
	t.Setenv("POSTGRES_PASSWORD", "p@ss")
	t.Setenv("POSTGRES_DB", "numan")
	t.Setenv("POSTGRES_SSLMODE", "require")
	t.Setenv("POSTGRES_MAX_OPEN_CONNS", "15")
	t.Setenv("POSTGRES_READ_MAX_OPEN_CONNS", "30")
	t.Setenv("POSTGRES_READ_MAX_IDLE_CONNS", "20")

	cfg, enabled := ResolvePostgresReadConfig()
	require.True(t, enabled)
	assert.Equal(t, "postgres-ro", cfg.Host)
	assert.Equal(t, "5433", cfg.Port)
	assert.Equal(t, "numan", cfg.DBName)
	assert.Equal(t, "require", cfg.SSLMode)
	assert.Equal(t, 30, cfg.MaxOpenConns)
	assert.Equal(t, 20, cfg.MaxIdleConns)
	assert.Equal(t, "postgres://numan:p%40ss@postgres-ro:5433/numan?sslmode=require", cfg.DSN)
}

func TestResolvePostgresReadConfig_ExplicitReadDSNAndClamp(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("DB_READ_DSN", "postgres://ro:pw@postgres-r:5432/numan?sslmode=require")
	t.Setenv("POSTGRES_READ_MAX_OPEN_CONNS", "6")
	t.Setenv("POSTGRES_READ_MAX_IDLE_CONNS", "50") // clampé à 6

	cfg, enabled := ResolvePostgresReadConfig()
	require.True(t, enabled)
	assert.Equal(t, "postgres://ro:pw@postgres-r:5432/numan?sslmode=require", cfg.DSN)
	assert.Equal(t, 6, cfg.MaxOpenConns)
	assert.Equal(t, 6, cfg.MaxIdleConns)
}

type noopConnector struct{}

func (noopConnector) Connect(context.Context) (driver.Conn, error) { return noopConn{}, nil }
func (noopConnector) Driver() driver.Driver                        { return noopDriver{} }

type noopDriver struct{}

func (noopDriver) Open(string) (driver.Conn, error) { return nil, errors.New("not implemented") }

type noopConn struct{}

func (noopConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not implemented") }
func (noopConn) Close() error                        { return nil }
func (noopConn) Begin() (driver.Tx, error)           { return nil, errors.New("not implemented") }

func TestReadDBAndWriteDB_AccessorsAndFallback(t *testing.T) {
	prevW, prevR := PostgresDB, PostgresReadDB
	t.Cleanup(func() {
		PostgresDB = prevW
		PostgresReadDB = prevR
	})

	primary := sql.OpenDB(noopConnector{})
	replica := sql.OpenDB(noopConnector{})
	t.Cleanup(func() {
		_ = primary.Close()
		_ = replica.Close()
	})

	// Fallback quand PostgresReadDB est nil
	PostgresDB = primary
	PostgresReadDB = nil
	assert.Same(t, primary, WriteDB())
	assert.Same(t, primary, ReadDB())
	assert.False(t, HasDedicatedReadPool())

	// Mode pool partagé (all-in-one)
	PostgresReadDB = primary
	assert.Same(t, primary, WriteDB())
	assert.Same(t, primary, ReadDB())
	assert.False(t, HasDedicatedReadPool())

	// Mode HA 2 pools distincts
	PostgresReadDB = replica
	assert.Same(t, primary, WriteDB())
	assert.Same(t, replica, ReadDB())
	assert.True(t, HasDedicatedReadPool())
}

func TestClosePostgres_NilSafeAndDualPool(t *testing.T) {
	prevW, prevR := PostgresDB, PostgresReadDB
	t.Cleanup(func() {
		PostgresDB = prevW
		PostgresReadDB = prevR
	})

	PostgresDB = nil
	PostgresReadDB = nil
	assert.NoError(t, ClosePostgres())

	// Mode partagé (ne ferme pas deux fois)
	shared := sql.OpenDB(noopConnector{})
	PostgresDB = shared
	PostgresReadDB = shared
	assert.NoError(t, ClosePostgres())

	// Mode 2 pools distincts
	primary := sql.OpenDB(noopConnector{})
	replica := sql.OpenDB(noopConnector{})
	PostgresDB = primary
	PostgresReadDB = replica
	assert.NoError(t, ClosePostgres())
}
