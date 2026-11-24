package scheduler_service

import (
	"context"
	"strconv"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	repoPostgres "github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/vmihailenco/msgpack/v5"
)

const (
	sessionRetentionDays = 7
	syncLedgerMaxAgeDays = 30
	graphEdgeMinWeight   = 0.05
	dlqMaxAgeDays        = 7
	dlqMaxRetries        = 3
)

func registerCategoryAHygieneJobs(eng *Engine) {
	// 1. pg_expired_sessions_purge (DEEP_IDLE, GlobalSQL, 24h, SLA 48h)
	eng.Register(JobDefinition{
		Name:         "pg_expired_sessions_purge",
		Category:     "hygiene",
		Description:  "Batched DELETE of expired auth.sessions older than 7 days",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeGlobalSQL,
		MinInterval:  24 * time.Hour,
		MaxStaleness: 48 * time.Hour,
		Essential:    true,
		Fn:           runPGExpiredSessionsPurge,
	})

	// 2. redis_sync_ledger_prune (IDLE, LocalDC, 12h, SLA 36h)
	eng.Register(JobDefinition{
		Name:         "redis_sync_ledger_prune",
		Category:     "hygiene",
		Description:  "Prunes ZSET entries older than 30 days in UserSyncLedger and ConvMessageLedger",
		MinRegime:    scheduler_models.RegimeIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  12 * time.Hour,
		MaxStaleness: 36 * time.Hour,
		Essential:    true,
		Fn:           runRedisSyncLedgerPrune,
	})

	// 3. redis_graph_edges_prune (DEEP_IDLE, LocalDC, 24h, SLA 72h)
	eng.Register(JobDefinition{
		Name:         "redis_graph_edges_prune",
		Category:     "hygiene",
		Description:  "Cursor-based SCAN of graph_cache:tag_edges:* to decay and prune dead Markov edges",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  24 * time.Hour,
		MaxStaleness: 72 * time.Hour,
		Fn:           runRedisGraphEdgesPrune,
	})

	// 4. articles_expired_evict_and_media_gc (MODERATE, LocalDC, 6h, SLA 24h)
	eng.Register(JobDefinition{
		Name:         "articles_expired_evict_and_media_gc",
		Category:     "hygiene",
		Description:  "Evicts expired articles from TTL-0 Redis L1 index and audits unreferenced article media",
		MinRegime:    scheduler_models.RegimeModerate,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  6 * time.Hour,
		MaxStaleness: 24 * time.Hour,
		Fn:           runArticlesExpiredEvictAndMediaGC,
	})

	// 5. redis_lex_autocomplete_audit (DEEP_IDLE, LocalDC, 24h, SLA 72h)
	eng.Register(JobDefinition{
		Name:         "redis_lex_autocomplete_audit",
		Category:     "hygiene",
		Description:  "Audits TTL-0 autocomplete ZSETs (UsersLex, CommunitiesLex, TagsLex) for stale entries",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  24 * time.Hour,
		MaxStaleness: 72 * time.Hour,
		Fn:           runRedisLexAutocompleteAudit,
	})

	// 6. expired_temp_bans_reconcile (MODERATE, GlobalSQL, 1h, SLA 6h)
	eng.Register(JobDefinition{
		Name:         "expired_temp_bans_reconcile",
		Category:     "hygiene",
		Description:  "Lifts expired temporary user bans in L3 Postgres and invalidates L1 user cache",
		MinRegime:    scheduler_models.RegimeModerate,
		LockScope:    scheduler_models.LockScopeGlobalSQL,
		MinInterval:  1 * time.Hour,
		MaxStaleness: 6 * time.Hour,
		Essential:    true,
		Fn:           runExpiredTempBansReconcile,
	})

	// 7. cuckoo_filter_health_and_rebuild (DEEP_IDLE, LocalDC, 24h, SLA 72h)
	eng.Register(JobDefinition{
		Name:         "cuckoo_filter_health_and_rebuild",
		Category:     "hygiene",
		Description:  "Checks GlobalCuckoo fill ratio (>=80% alert) and rebuilds filter cleanly from L3",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  24 * time.Hour,
		MaxStaleness: 72 * time.Hour,
		Fn:           runCuckooFilterHealthAndRebuild,
	})

	// 8. dlq_audit_and_replay (MODERATE, LocalDC, 6h, SLA 24h)
	eng.Register(JobDefinition{
		Name:         "dlq_audit_and_replay",
		Category:     "hygiene",
		Description:  "Audits redis.DLQ, replays transient recoverable events up to max retries, purges expired entries",
		MinRegime:    scheduler_models.RegimeModerate,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  6 * time.Hour,
		MaxStaleness: 24 * time.Hour,
		Essential:    true,
		Fn:           runDLQAuditAndReplay,
	})
}

func runPGExpiredSessionsPurge(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
	batchSize := 5000
	if trickle {
		batchSize = 500
	}
	cutoffMs := domain.NowMillis() - int64((sessionRetentionDays*24*time.Hour)/time.Millisecond)
	const maxChunksPerRun = 20

	for chunk := 0; chunk < maxChunksPerRun; chunk++ {
		if ctx.Err() != nil {
			return cp, false, ctx.Err()
		}
		deleted, err := repoPostgres.FuncPurgeExpiredSessions(ctx, cutoffMs, batchSize)
		if err != nil {
			return cp, false, err
		}
		cp.ItemsDone += deleted
		if deleted < int64(batchSize) {
			return cp, true, nil
		}
		if err := pauseIfTrickle(ctx, trickle); err != nil {
			return cp, false, err
		}
	}
	return cp, true, nil
}

func runRedisSyncLedgerPrune(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
	cutoffMs := domain.NowMillis() - int64((syncLedgerMaxAgeDays*24*time.Hour)/time.Millisecond)
	done, total, err := cache_service.PruneSyncLedgersBefore(ctx, cutoffMs, func(d, _ int64) error {
		if trickle && d%50 == 0 {
			return pauseIfTrickle(ctx, true)
		}
		return nil
	})
	cp.ItemsDone += done
	cp.ItemsTotal += total
	if err != nil {
		return cp, false, err
	}
	return cp, true, nil
}

func runRedisGraphEdgesPrune(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
	done, total, err := cache_service.PruneDeadGraphEdges(ctx, graphEdgeMinWeight, func(d, _ int64) error {
		if trickle && d%50 == 0 {
			return pauseIfTrickle(ctx, true)
		}
		return nil
	})
	cp.ItemsDone += done
	cp.ItemsTotal = total
	if err != nil {
		return cp, false, err
	}
	return cp, true, nil
}

func runArticlesExpiredEvictAndMediaGC(ctx context.Context, cp scheduler_models.JobCheckpoint, _ bool) (scheduler_models.JobCheckpoint, bool, error) {
	allRaw, err := cache_service.ScanArticlesIndexRaw(ctx)
	if err != nil || len(allRaw) == 0 {
		return cp, true, nil
	}
	nowMs := domain.NowMillis()
	cp.ItemsTotal = int64(len(allRaw))

	var expiredIDs []int64
	var candidateMediaIDs []int64
	for field, raw := range allRaw {
		if ctx.Err() != nil {
			return cp, false, ctx.Err()
		}
		var art article_models.ArticlePayload
		if errU := msgpack.Unmarshal([]byte(raw), &art); errU != nil {
			if id, errP := strconv.ParseInt(field, 10, 64); errP == nil && id > 0 {
				expiredIDs = append(expiredIDs, id)
			}
			continue
		}
		if art.ExpiresAt != nil && *art.ExpiresAt > 0 && *art.ExpiresAt <= nowMs {
			expiredIDs = append(expiredIDs, art.ID)
			if art.CoverMediaID > 0 {
				candidateMediaIDs = append(candidateMediaIDs, art.CoverMediaID)
			}
			if art.MarkdownMediaID > 0 {
				candidateMediaIDs = append(candidateMediaIDs, art.MarkdownMediaID)
			}
			candidateMediaIDs = append(candidateMediaIDs, art.AttachmentMediaIDs...)
		}
		cp.ItemsDone++
	}

	if len(expiredIDs) > 0 {
		_ = object_cache_service.DeleteArticlesBatchFromCache(ctx, expiredIDs)
	}
	if len(candidateMediaIDs) > 0 {
		if unref, errPG := repoPostgres.FuncFilterUnreferencedArticleMedia(ctx, candidateMediaIDs, 0); errPG == nil && len(unref) > 0 {
			numan_log.Info(ctx).Int("unreferenced_article_media", len(unref)).Msg("Scheduler: médias d'articles expirés détectés sans référence active")
		}
	}
	return cp, true, nil
}

func runRedisLexAutocompleteAudit(ctx context.Context, cp scheduler_models.JobCheckpoint, _ bool) (scheduler_models.JobCheckpoint, bool, error) {
	done, total, err := cache_service.AuditLexAutocompleteCaches(ctx)
	cp.ItemsDone += done
	cp.ItemsTotal += total
	if err != nil {
		return cp, false, err
	}
	return cp, true, nil
}

func runExpiredTempBansReconcile(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
	nowMs := domain.NowMillis()
	batchSize := 500
	if trickle {
		batchSize = 100
	}
	userIDs, err := repoPostgres.FuncReconcileExpiredTempBans(ctx, nowMs, batchSize)
	if err != nil {
		return cp, false, err
	}
	cp.ItemsTotal = int64(len(userIDs))
	for _, uid := range userIDs {
		if ctx.Err() != nil {
			return cp, false, ctx.Err()
		}
		cache_service.InvalidateUserProfilesFromCache(ctx, uid)
		cp.ItemsDone++
	}
	return cp, true, nil
}

func runCuckooFilterHealthAndRebuild(ctx context.Context, cp scheduler_models.JobCheckpoint, _ bool) (scheduler_models.JobCheckpoint, bool, error) {
	ratio, ok := service.GetGlobalCuckooFillRatio()
	if !ok {
		return cp, true, nil
	}
	if ratio >= 0.80 {
		numan_log.Warn(ctx).Float64("cuckoo_fill_ratio", ratio).Msg("Scheduler: taux de remplissage Cuckoo Filter >= 80%, reconstruction propre déclenchée")
	}
	if ctx.Err() != nil {
		return cp, false, ctx.Err()
	}
	if ratio >= 0.80 || cp.ItemsDone == 0 {
		if err := service.RebuildCuckooFilterSafe(ctx); err != nil {
			numan_log.Warn(ctx).Err(err).Msg("Scheduler: reconstruction Cuckoo Filter ignorée (PostgreSQL indisponible)")
			return cp, true, nil
		}
		cp.ItemsDone = 1
		cp.ItemsTotal = 1
	}
	return cp, true, nil
}

func runDLQAuditAndReplay(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
	limit := int64(200)
	if trickle {
		limit = 50
	}
	cutoffMs := domain.NowMillis() - int64((dlqMaxAgeDays*24*time.Hour)/time.Millisecond)
	done, total, err := cache_service.AuditAndReplayDLQ(ctx, limit, cutoffMs, dlqMaxRetries)
	cp.ItemsDone += done
	cp.ItemsTotal = total
	if err != nil {
		return cp, false, err
	}
	return cp, true, nil
}
