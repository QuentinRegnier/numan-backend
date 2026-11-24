package scheduler_service

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	repoPostgres "github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
)

const (
	defaultSoftDeleteRetentionDays = 30
	abortedMultipartMaxAge         = 24 * time.Hour
)

func registerCategoryBDatabaseJobs(eng *Engine) {
	// 9. pg_vacuum_analyze_hot_tables (DEEP_IDLE, GlobalSQL, 24h, SLA 72h)
	eng.Register(JobDefinition{
		Name:         "pg_vacuum_analyze_hot_tables",
		Category:     "database",
		Description:  "Runs targeted VACUUM (ANALYZE) (never VACUUM FULL) with safe statement_timeout on hot tables",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeGlobalSQL,
		MinInterval:  24 * time.Hour,
		MaxStaleness: 72 * time.Hour,
		Essential:    true,
		Fn:           runPGVacuumAnalyzeHotTables,
	})

	// 10. pg_mongo_old_soft_deletes_purge (DEEP_IDLE, GlobalSQL, 24h, SLA 72h)
	eng.Register(JobDefinition{
		Name:         "pg_mongo_old_soft_deletes_purge",
		Category:     "database",
		Description:  "Batched RGPD hard-delete of aged soft-deleted rows in Postgres and residual Mongo L2 docs",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeGlobalSQL,
		MinInterval:  24 * time.Hour,
		MaxStaleness: 72 * time.Hour,
		Fn:           runPGMongoOldSoftDeletesPurge,
	})

	// 11. l1_l2_l3_counter_self_healing (DEEP_IDLE, GlobalSQL, 24h, SLA 72h)
	eng.Register(JobDefinition{
		Name:         "l1_l2_l3_counter_self_healing",
		Category:     "database",
		Description:  "Reconciles derived counters (POI ratings/reviews, Post likes/comments) against L3 source of truth",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeGlobalSQL,
		MinInterval:  24 * time.Hour,
		MaxStaleness: 72 * time.Hour,
		Fn:           runCounterSelfHealing,
	})

	// 12. mongo_l2_hygiene_and_fragmentation_audit (DEEP_IDLE, LocalDC, 24h, SLA 72h)
	eng.Register(JobDefinition{
		Name:         "mongo_l2_hygiene_and_fragmentation_audit",
		Category:     "database",
		Description:  "Bounded Mongo L2 sliding-window hygiene and WiredTiger fragmentation audit (secondary-only compact)",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  24 * time.Hour,
		MaxStaleness: 72 * time.Hour,
		Fn:           runMongoL2HygieneAndFragmentationAudit,
	})

	// 13. storage_aborted_multipart_cleanup (DEEP_IDLE, GlobalSQL, 24h, SLA 72h)
	eng.Register(JobDefinition{
		Name:         "storage_aborted_multipart_cleanup",
		Category:     "database",
		Description:  "Aborts incomplete S3/MinIO multipart uploads older than 24h to prevent phantom storage billing",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeGlobalSQL,
		MinInterval:  24 * time.Hour,
		MaxStaleness: 72 * time.Hour,
		Fn:           runStorageAbortedMultipartCleanup,
	})
}

func runPGVacuumAnalyzeHotTables(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
	tables := repoPostgres.HotTablesAllowlist
	cp.ItemsTotal = int64(len(tables))
	startIdx := int(cp.LastID)
	if startIdx < 0 || startIdx >= len(tables) {
		startIdx = 0
	}

	timeout := 30 * time.Second
	if trickle {
		timeout = 10 * time.Second
	}

	for i := startIdx; i < len(tables); i++ {
		if ctx.Err() != nil {
			cp.LastID = int64(i)
			return cp, false, ctx.Err()
		}
		if err := repoPostgres.FuncVacuumAnalyzeTable(ctx, tables[i], timeout); err != nil {
			numan_log.Warn(ctx).Err(err).Str("table", tables[i]).Msg("Scheduler: échec VACUUM (ANALYZE) sur table chaude")
		}
		cp.LastID = int64(i + 1)
		cp.ItemsDone = int64(i + 1)
		if err := pauseIfTrickle(ctx, trickle); err != nil {
			return cp, false, err
		}
	}
	return cp, true, nil
}

func runPGMongoOldSoftDeletesPurge(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
	retentionDays := defaultSoftDeleteRetentionDays
	if raw := strings.TrimSpace(os.Getenv("SCHEDULER_SOFT_DELETE_RETENTION_DAYS")); raw != "" {
		if d, err := strconv.Atoi(raw); err == nil && d >= 7 {
			retentionDays = d
		}
	}
	cutoffMs := domain.NowMillis() - int64((time.Duration(retentionDays)*24*time.Hour)/time.Millisecond)
	batchSize := 500
	if trickle {
		batchSize = 100
	}

	for chunk := 0; chunk < 10; chunk++ {
		if ctx.Err() != nil {
			return cp, false, ctx.Err()
		}
		deleted, err := repoPostgres.FuncPurgeOldSoftDeletes(ctx, cutoffMs, batchSize)
		if err != nil {
			return cp, false, err
		}
		cp.ItemsDone += deleted
		if deleted < int64(batchSize) {
			break
		}
		if err := pauseIfTrickle(ctx, trickle); err != nil {
			return cp, false, err
		}
	}

	if ctx.Err() == nil {
		mongoDeleted, err := repoMongo.FuncPurgeOldSoftDeletes(ctx, cutoffMs, []string{"posts", "comments", "pois", "poi_reviews", "articles"})
		if err != nil {
			numan_log.Warn(ctx).Err(err).Msg("Scheduler: échec partiel de purge soft-deletes MongoDB L2")
		} else {
			cp.ItemsDone += mongoDeleted
		}
	}

	return cp, true, nil
}

func runCounterSelfHealing(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
	batchSize := 200
	if trickle {
		batchSize = 50
	}
	fixed, err := repoPostgres.FuncReconcileDerivedCounters(ctx, batchSize)
	if err != nil {
		return cp, false, err
	}
	cp.ItemsDone += fixed
	cp.ItemsTotal = cp.ItemsDone
	return cp, true, nil
}

func runMongoL2HygieneAndFragmentationAudit(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
	collections := []string{"posts", "messages", "comments", "notifications", "users"}
	cp.ItemsTotal = int64(len(collections))

	allowCompact := strings.EqualFold(strings.TrimSpace(os.Getenv("SCHEDULER_MONGO_COMPACT_ENABLED")), "true")
	statsList, err := repoMongo.FuncAuditCollectionStats(ctx, collections)
	if err != nil {
		return cp, false, err
	}
	for _, st := range statsList {
		if ctx.Err() != nil {
			return cp, false, ctx.Err()
		}
		if st.StorageBytes > 0 {
			wasteRatio := 1.0 - (float64(st.SizeBytes) / float64(st.StorageBytes))
			if wasteRatio > 0.40 {
				numan_log.Warn(ctx).
					Str("collection", st.Collection).
					Float64("waste_ratio", wasteRatio).
					Bool("compact_enabled", allowCompact).
					Msg("Scheduler: fragmentation WiredTiger MongoDB > 40%")
				if allowCompact {
					_ = repoMongo.FuncCompactCollectionIfSecondary(ctx, st.Collection)
				}
			}
		}
		cp.ItemsDone++
		if err := pauseIfTrickle(ctx, trickle); err != nil {
			return cp, false, err
		}
	}
	return cp, true, nil
}

func runStorageAbortedMultipartCleanup(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
	maxItems := 200
	if trickle {
		maxItems = 50
	}
	aborted, err := media_service.CleanupAbortedMultipartUploads(ctx, abortedMultipartMaxAge, maxItems)
	cp.ItemsDone += aborted
	if err != nil {
		return cp, false, err
	}
	return cp, true, nil
}
