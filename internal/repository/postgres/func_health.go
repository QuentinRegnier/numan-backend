package postgres

import (
	"context"
	"database/sql"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	postgresInfra "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// FuncPingWrite vérifie la connectivité du pool PostgreSQL Primary (Write).
func FuncPingWrite(ctx context.Context) error {
	db := postgresInfra.WriteDB()
	if db == nil {
		return numan_error.NewAppError(503, numan_error.CodeDatabaseError, "Pool PostgreSQL Primary non initialisé", nil)
	}
	if err := db.PingContext(ctx); err != nil {
		return numan_error.NewAppError(503, numan_error.CodeDatabaseError, "PostgreSQL Primary indisponible", err)
	}
	return nil
}

// FuncHasDedicatedReadPool indique si un pool Read Replica PostgreSQL distinct est configuré.
func FuncHasDedicatedReadPool() bool {
	return postgresInfra.HasDedicatedReadPool()
}

// FuncPingRead vérifie la connectivité du pool PostgreSQL Read Replica.
func FuncPingRead(ctx context.Context) error {
	db := postgresInfra.ReadDB()
	if db == nil {
		return numan_error.NewAppError(503, numan_error.CodeDatabaseError, "Pool PostgreSQL Read non initialisé", nil)
	}
	if err := db.PingContext(ctx); err != nil {
		return numan_error.NewAppError(503, numan_error.CodeDatabaseError, "PostgreSQL Read indisponible", err)
	}
	return nil
}

// FuncWritePoolStats retourne les statistiques sql.DBStats du pool Primary (Write).
func FuncWritePoolStats() (sql.DBStats, bool) {
	db := postgresInfra.WriteDB()
	if db == nil {
		return sql.DBStats{}, false
	}
	return db.Stats(), true
}

// FuncReadPoolStats retourne les statistiques sql.DBStats du pool Read Replica.
func FuncReadPoolStats() (sql.DBStats, bool) {
	db := postgresInfra.ReadDB()
	if db == nil {
		return sql.DBStats{}, false
	}
	return db.Stats(), true
}
