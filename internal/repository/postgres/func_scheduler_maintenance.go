package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	postgresInfra "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
)

// HotTablesAllowlist définit la liste blanche stricte des tables éligibles à VACUUM (ANALYZE)
// (JAMAIS de VACUUM FULL !).
var HotTablesAllowlist = []string{
	"content.posts",
	"content.likes",
	"content.comments",
	"messaging.messages",
	"auth.sessions",
	"content.poi_reviews",
	"moderation.reports",
}

func isAllowedHotTable(table string) bool {
	for _, allowed := range HotTablesAllowlist {
		if allowed == table {
			return true
		}
	}
	return false
}

// FuncPurgeExpiredSessions supprime par lots les sessions expirées antérieures à cutoffMs.
func FuncPurgeExpiredSessions(ctx context.Context, cutoffMs int64, limit int) (int64, error) {
	db := postgresInfra.WriteDB()
	if db == nil {
		return 0, nil
	}
	var deleted int64
	err := db.QueryRowContext(ctx, "SELECT auth.func_purge_expired_sessions($1, $2)", cutoffMs, limit).Scan(&deleted)
	if err != nil {
		numan_log.Error(ctx).Err(err).Int64("cutoff_ms", cutoffMs).Msg("Erreur SQL func_purge_expired_sessions")
		return 0, numan_error.NewAppError(500, numan_error.CodeDatabaseError, "Erreur purge sessions expirées", err)
	}
	return deleted, nil
}

// FuncReconcileExpiredTempBans lève les bannissements temporaires expirés et retourne les user_id réconciliés.
func FuncReconcileExpiredTempBans(ctx context.Context, nowMs int64, limit int) ([]int64, error) {
	db := postgresInfra.WriteDB()
	if db == nil {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, "SELECT user_id FROM auth.func_reconcile_expired_temp_bans($1, $2)", nowMs, limit)
	if err != nil {
		numan_log.Error(ctx).Err(err).Int64("now_ms", nowMs).Msg("Erreur SQL func_reconcile_expired_temp_bans")
		return nil, numan_error.NewAppError(500, numan_error.CodeDatabaseError, "Erreur réconciliation bannissements temporaires", err)
	}
	defer rows.Close()

	var userIDs []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, numan_error.NewAppError(500, numan_error.CodeDatabaseError, "Erreur lecture user_id débanni", err)
		}
		userIDs = append(userIDs, uid)
	}
	return userIDs, rows.Err()
}

// FuncPurgeOldSoftDeletes exécute la purge RGPD par lots des enregistrements soft-deleted anciens.
func FuncPurgeOldSoftDeletes(ctx context.Context, cutoffMs int64, limit int) (int64, error) {
	db := postgresInfra.WriteDB()
	if db == nil {
		return 0, nil
	}
	var deleted int64
	err := db.QueryRowContext(ctx, "SELECT util.func_purge_old_soft_deletes($1, $2)", cutoffMs, limit).Scan(&deleted)
	if err != nil {
		numan_log.Error(ctx).Err(err).Int64("cutoff_ms", cutoffMs).Msg("Erreur SQL func_purge_old_soft_deletes")
		return 0, numan_error.NewAppError(500, numan_error.CodeDatabaseError, "Erreur purge soft-deletes anciens", err)
	}
	return deleted, nil
}

// FuncReconcileDerivedCounters réconcilie par lots les compteurs dérivés (POIs, Posts) contre la source L3.
func FuncReconcileDerivedCounters(ctx context.Context, limit int) (int64, error) {
	db := postgresInfra.WriteDB()
	if db == nil {
		return 0, nil
	}
	var fixed int64
	err := db.QueryRowContext(ctx, "SELECT util.func_reconcile_derived_counters($1)", limit).Scan(&fixed)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Erreur SQL func_reconcile_derived_counters")
		return 0, numan_error.NewAppError(500, numan_error.CodeDatabaseError, "Erreur auto-guérison compteurs dérivés", err)
	}
	return fixed, nil
}

// FuncVacuumAnalyzeTable exécute un VACUUM (ANALYZE) non-bloquant (JAMAIS VACUUM FULL) sur une table de la liste blanche,
// avec un statement_timeout de sécurité appliqué sur une connexion dédiée.
func FuncVacuumAnalyzeTable(ctx context.Context, tableName string, statementTimeout time.Duration) error {
	if !isAllowedHotTable(tableName) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Table non autorisée pour VACUUM ANALYZE: "+tableName, nil)
	}
	db := postgresInfra.WriteDB()
	if db == nil {
		return nil
	}
	if statementTimeout <= 0 {
		statementTimeout = 30 * time.Second
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return numan_error.NewAppError(500, numan_error.CodeDatabaseError, "Impossible d'ouvrir une connexion dédiée VACUUM", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "RESET statement_timeout")
		_ = conn.Close()
	}()

	timeoutMs := statementTimeout.Milliseconds()
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET statement_timeout = '%dms'", timeoutMs)); err != nil {
		return numan_error.NewAppError(500, numan_error.CodeDatabaseError, "Impossible de configurer statement_timeout", err)
	}
	if _, err := conn.ExecContext(ctx, "VACUUM (ANALYZE) "+tableName); err != nil {
		return numan_error.NewAppError(500, numan_error.CodeDatabaseError, "Erreur VACUUM ANALYZE sur "+tableName, err)
	}
	return nil
}

// FuncAcquireSchedulerAdvisoryLock acquiert un verrou consultatif PostgreSQL global sur une connexion dédiée
// via les fonctions SQL `util.func_try_scheduler_advisory_lock` et `util.func_release_scheduler_advisory_lock`.
// Retourne (nil, false, nil) lorsque WriteDB() n'est pas initialisé ou si le verrou est déjà détenu.
func FuncAcquireSchedulerAdvisoryLock(ctx context.Context, lockID int64) (release func(context.Context), acquired bool, hasDB bool, err error) {
	db := postgresInfra.WriteDB()
	if db == nil {
		return nil, false, false, nil
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, false, true, numan_error.NewAppError(500, numan_error.CodeDatabaseError, "Impossible d'ouvrir une connexion dédiée pour le verrou scheduler", err)
	}

	var locked bool
	if err := conn.QueryRowContext(ctx, "SELECT util.func_try_scheduler_advisory_lock($1)", lockID).Scan(&locked); err != nil {
		_ = conn.Close()
		return nil, false, true, numan_error.NewAppError(500, numan_error.CodeDatabaseError, "Erreur acquisition verrou consultatif scheduler", err)
	}
	if !locked {
		_ = conn.Close()
		return nil, false, true, nil
	}

	return func(relCtx context.Context) {
		unlockCtx, cancel := context.WithTimeout(relCtx, 5*time.Second)
		defer cancel()
		var unlocked bool
		if err := conn.QueryRowContext(unlockCtx, "SELECT util.func_release_scheduler_advisory_lock($1)", lockID).Scan(&unlocked); err != nil {
			numan_log.Warn(relCtx).Err(err).Int64("lock_id", lockID).Msg("Échec libération verrou consultatif scheduler PostgreSQL")
		}
		_ = conn.Close()
	}, true, true, nil
}
