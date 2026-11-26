package scheduler_service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/worker"
)

func registerCategoryCAlgoAndMigratedJobs(eng *Engine) {
	// 14. lsh_affinity_matrix_reindex (DEEP_IDLE, LocalDC, 24h, SLA 72h)
	eng.Register(JobDefinition{
		Name:         "lsh_affinity_matrix_reindex",
		Category:     "algo_cache",
		Description:  "Prunes deleted/expired post IDs from LSH affinity buckets in bounded checkpointed chunks",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  24 * time.Hour,
		MaxStaleness: 72 * time.Hour,
		Fn:           runLSHAffinityMatrixReindex,
	})

	// 15. social_graph_2hop_3hop_suggestions (DEEP_IDLE, LocalDC, 24h, SLA 72h)
	eng.Register(JobDefinition{
		Name:         "social_graph_2hop_3hop_suggestions",
		Category:     "algo_cache",
		Description:  "Offline 2-hop/3-hop mutual friend affinity traversal enriching SpeedAddable without impacting live APIs",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  24 * time.Hour,
		MaxStaleness: 72 * time.Hour,
		Fn:           runSocialGraph2Hop3HopSuggestions,
	})

	// 16. most_cache_surgical_purify (DEEP_IDLE, LocalDC, 12h, SLA 48h)
	eng.Register(JobDefinition{
		Name:         "most_cache_surgical_purify",
		Category:     "algo_cache",
		Description:  "Surgically removes deleted/moderated posts from RankedPosts and TagPosts without resetting viral scores",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  12 * time.Hour,
		MaxStaleness: 48 * time.Hour,
		Fn:           runMostCacheSurgicalPurify,
	})

	// 17. object_pool_morning_prewarm (DEEP_IDLE, LocalDC, window 04:00-06:00 DC local time, 24h)
	eng.Register(JobDefinition{
		Name:            "object_pool_morning_prewarm",
		Category:        "algo_cache",
		Description:     "Pre-hydrates top hot posts into Tier 2 Redis Object Pool when fill_ratio < 0.70 (04:00-06:00 local)",
		MinRegime:       scheduler_models.RegimeDeepIdle,
		LockScope:       scheduler_models.LockScopeLocalDC,
		MinInterval:     24 * time.Hour,
		MaxStaleness:    48 * time.Hour,
		WindowStartHour: 4,
		WindowEndHour:   6,
		Fn:              runObjectPoolMorningPrewarm,
	})

	// 18. Migration des 7 crons périodiques existants (internal/worker/) dans l'Activity-Aware Scheduler
	eng.Register(JobDefinition{
		Name:         "hashtag_canon",
		Category:     "migrated_cron",
		Description:  "Migrated cron: Levenshtein hashtag typo clustering and alias merging",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  12 * time.Hour,
		MaxStaleness: 48 * time.Hour,
		Fn: func(ctx context.Context, cp scheduler_models.JobCheckpoint, _ bool) (scheduler_models.JobCheckpoint, bool, error) {
			worker.RunHashtagCanonicalizationCycle(ctx)
			cp.ItemsDone = 1
			cp.ItemsTotal = 1
			return cp, true, nil
		},
	})

	eng.Register(JobDefinition{
		Name:         "user_recommendation_refresh",
		Category:     "migrated_cron",
		Description:  "Migrated cron: 24-48h batch user recommendation refresh",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  24 * time.Hour,
		MaxStaleness: 48 * time.Hour,
		Fn: func(ctx context.Context, cp scheduler_models.JobCheckpoint, _ bool) (scheduler_models.JobCheckpoint, bool, error) {
			stats := worker.RunUserRecommendationRefreshCronCycle(ctx)
			cp.ItemsDone = int64(stats.JobsEnqueued)
			cp.ItemsTotal = int64(stats.UsersScanned)
			return cp, true, nil
		},
	})

	eng.Register(JobDefinition{
		Name:         "community_recommendation_refresh",
		Category:     "migrated_cron",
		Description:  "Migrated cron: per-language global public community scoring",
		MinRegime:    scheduler_models.RegimeIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  2 * time.Hour,
		MaxStaleness: 24 * time.Hour,
		Fn: func(ctx context.Context, cp scheduler_models.JobCheckpoint, _ bool) (scheduler_models.JobCheckpoint, bool, error) {
			stats, err := worker.RunCommunityRecommendationCronCycle(ctx)
			cp.ItemsDone = int64(stats.CommunitiesProcessed)
			cp.ItemsTotal = int64(stats.CommunitiesProcessed)
			return cp, err == nil, err
		},
	})

	eng.Register(JobDefinition{
		Name:         "feed_cache_warmup",
		Category:     "migrated_cron",
		Description:  "Migrated cron: inactive user feed cache warm-up",
		MinRegime:    scheduler_models.RegimeIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  1 * time.Hour,
		MaxStaleness: 24 * time.Hour,
		Fn: func(ctx context.Context, cp scheduler_models.JobCheckpoint, _ bool) (scheduler_models.JobCheckpoint, bool, error) {
			worker.RunFeedWarmupCycle(ctx)
			cp.ItemsDone = 1
			cp.ItemsTotal = 1
			return cp, true, nil
		},
	})

	eng.Register(JobDefinition{
		Name:         "orphan_cleanup_gc",
		Category:     "migrated_cron",
		Description:  "Migrated cron: 4 orphan garbage collectors (media, likes, reactions, saved)",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeGlobalSQL,
		MinInterval:  24 * time.Hour,
		MaxStaleness: 48 * time.Hour,
		Essential:    true,
		Fn: func(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
			steps := []func(context.Context){
				worker.RunMediaCleanupCycle,
				worker.RunLikeCleanupCycle,
				worker.RunReactionCleanupCycle,
				worker.RunSavedCleanupCycle,
			}
			cp.ItemsTotal = int64(len(steps))
			for idx, fn := range steps {
				if ctx.Err() != nil {
					cp.LastID = int64(idx)
					return cp, false, ctx.Err()
				}
				fn(ctx)
				cp.ItemsDone = int64(idx + 1)
				if err := pauseIfTrickle(ctx, trickle); err != nil {
					return cp, false, err
				}
			}
			return cp, true, nil
		},
	})

	eng.Register(JobDefinition{
		Name:         "trust_and_moderation_aging",
		Category:     "migrated_cron",
		Description:  "Migrated cron: monthly trust score +0.02 bonus and quadratic moderation aging",
		MinRegime:    scheduler_models.RegimeIdle,
		LockScope:    scheduler_models.LockScopeGlobalSQL,
		MinInterval:  6 * time.Hour,
		MaxStaleness: 48 * time.Hour,
		Fn: func(ctx context.Context, cp scheduler_models.JobCheckpoint, _ bool) (scheduler_models.JobCheckpoint, bool, error) {
			worker.ProcessMonthlyCleanBonus(ctx)
			if ctx.Err() != nil {
				return cp, false, ctx.Err()
			}
			aged, err := worker.ProcessModerationAgingBatch(ctx, time.Now())
			cp.ItemsDone = int64(aged)
			cp.ItemsTotal = int64(aged)
			return cp, err == nil, err
		},
	})

	eng.Register(JobDefinition{
		Name:         "algo_time_decay_deep",
		Category:     "migrated_cron",
		Description:  "Migrated cron: deep time-decay pass on aging posts (24h..30d) in DEEP_IDLE",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  6 * time.Hour,
		MaxStaleness: 48 * time.Hour,
		Fn: func(ctx context.Context, cp scheduler_models.JobCheckpoint, _ bool) (scheduler_models.JobCheckpoint, bool, error) {
			n, err := worker.RunTimeDecayDeepPass(ctx)
			cp.ItemsDone = n
			cp.ItemsTotal = n
			return cp, err == nil, err
		},
	})

	// 19. redis_orphan_cleanup (DEEP_IDLE, LocalDC, 24h, SLA 72h)
	eng.Register(JobDefinition{
		Name:         "redis_orphan_cleanup",
		Category:     "algo_cache",
		Description:  "Cluster-safe SCAN + Snowflake grace-window + batched Postgres L3 verification + UNLINK orphan Redis keys (dry-run by default)",
		MinRegime:    scheduler_models.RegimeDeepIdle,
		LockScope:    scheduler_models.LockScopeLocalDC,
		MinInterval:  24 * time.Hour,
		MaxStaleness: 72 * time.Hour,
		Fn:           runRedisOrphanCleanup,
	})
}

func runRedisOrphanCleanup(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
	summary, err := worker.RunRedisOrphanCleanupCycle(ctx, func(_ int64) error {
		return pauseIfTrickle(ctx, trickle)
	})
	cp.ItemsDone = summary.OrphansUnlinked
	cp.ItemsTotal = summary.KeysScanned
	if err != nil {
		return cp, false, err
	}
	return cp, true, nil
}

func runLSHAffinityMatrixReindex(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
	done, total, err := cache_service.PruneLSHAffinityBuckets(ctx, func(d, _ int64) error {
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

func runSocialGraph2Hop3HopSuggestions(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
	done, total, err := cache_service.EnrichSocialGraph2HopSuggestions(ctx, func(d, _ int64) error {
		if trickle && d%25 == 0 {
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

func runMostCacheSurgicalPurify(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (scheduler_models.JobCheckpoint, bool, error) {
	done, total, err := cache_service.SurgicallyPurifyMostCache(ctx, func(d, _ int64) error {
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

func runObjectPoolMorningPrewarm(ctx context.Context, cp scheduler_models.JobCheckpoint, _ bool) (scheduler_models.JobCheckpoint, bool, error) {
	if fill := cache_service.ObjectPoolMaxFillRatio(ctx); fill >= 0.70 {
		numan_log.Info(ctx).Float64("fill_ratio", fill).Msg("Scheduler: Object Pool >= 70%, pre-warm matinal ignoré")
		return cp, true, nil
	}
	ids, err := cache_service.GetTopRecentRankedPostIDs(ctx, 200)
	if err != nil || len(ids) == 0 {
		return cp, true, nil
	}
	cp.ItemsTotal = int64(len(ids))
	if ctx.Err() == nil {
		_, _ = object_cache_service.GetPostsView(ctx, ids)
		cp.ItemsDone = int64(len(ids))
	}
	return cp, true, nil
}
