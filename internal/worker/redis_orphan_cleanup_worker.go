package worker

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/metrics"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ============================================================================
// PHASE 4 : WORKER DE NETTOYAGE DES CLÉS ORPHELINES REDIS (SCAN + L3 + UNLINK)
// ============================================================================
//
// Garanties de sécurité :
//   1. Aucun `KEYS *` : parcours strictement non-bloquant via SCAN Cluster-safe (ForEachMaster).
//   2. Aucune suppression sur erreur : une erreur SQL/DB n'est JAMAIS interprétée comme une absence.
//   3. Fenêtre de grâce Snowflake (`RedisOrphanGraceWindow`, défaut 15m) : protège les écritures
//      récentes encore en transit dans `WorkerQueue` (Write-Behind) ou en réplication Postgres.
//   4. Préservation du Tombstone Pattern sur `redis.Posts` (`post_physical` vérifie uniquement
//      l'existence physique dans `content.posts` sans filtrer sur `visibility >= 0`).
//   5. Dry-Run activé par défaut (`RedisOrphanCleanerDryRun = true`, modifiable à chaud via `/admin/variables`).
//   6. Suppression asynchrone non-bloquante par lots bornés via `UNLINK` Cluster-safe.

const (
	redisOrphanTierCore   = "core"
	redisOrphanTierObject = "object"
)

// RedisOrphanTarget décrit une collection Redis éligible à la réconciliation L3 PostgreSQL.
type RedisOrphanTarget struct {
	Name       string
	Collection *redis.Collection
	Tier       string // "core" ou "object"
	Entity     string // constante postgres.OrphanEntity*
}

// RedisOrphanCleanupConfig paramètre un cycle de nettoyage des clés orphelines Redis.
type RedisOrphanCleanupConfig struct {
	DryRun            bool
	GraceWindow       time.Duration
	ScanCount         int64
	PostgresBatchSize int
	UnlinkBatchSize   int
	MaxKeysPerCycle   int
	Targets           []RedisOrphanTarget

	// Dépendances injectables pour les tests unitaires (valeurs par défaut en production).
	NowFn           func() time.Time
	CheckExistingFn func(ctx context.Context, entity string, ids []int64) (map[int64]bool, error)
	MetricsRegistry *metrics.Registry
	OnProgress      func(keysScanned int64) error
}

// RedisOrphanCleanupSummary résume les compteurs observés sur un cycle complet.
type RedisOrphanCleanupSummary struct {
	DryRun          bool
	KeysScanned     int64
	SkippedGrace    int64
	SkippedInvalid  int64
	OrphansDetected int64
	OrphansUnlinked int64
	Errors          int64
	BudgetReached   bool
	Duration        time.Duration
}

// DefaultRedisOrphanTargets retourne la liste ordonnée des 31 collections Redis
// indexées par identifiant Snowflake unique, en plaçant les 8 collections persistantes
// (TTL = 0) en priorité avant les caches à TTL borné.
func DefaultRedisOrphanTargets() []RedisOrphanTarget {
	return []RedisOrphanTarget{
		// ── Vague 1 (Prioritaire) : Clés persistantes (TTL = 0) ──────────────
		{Name: "UsersLite", Collection: redis.UsersLite, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityUser},
		{Name: "UserInbox", Collection: redis.UserInbox, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityUser},
		{Name: "InboxActivity", Collection: redis.InboxActivity, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityUser},
		{Name: "NotificationCursors", Collection: redis.NotificationCursors, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityUser},
		{Name: "NotificationActivity", Collection: redis.NotificationActivity, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityUser},
		{Name: "UserPublicComms", Collection: redis.UserPublicComms, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityUser},
		{Name: "ConvParticipants", Collection: redis.ConvParticipants, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityConversation},
		{Name: "POIs", Collection: redis.POIs, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityPOIActive},

		// ── Vague 2 : Clés Tier 1 Core à TTL borné ───────────────────────────
		{Name: "UserSyncLedger", Collection: redis.UserSyncLedger, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityUser},
		{Name: "TelemetryProfiles", Collection: redis.TelemetryProfiles, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityUser},
		{Name: "ContentVectors", Collection: redis.ContentVectors, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityPostActive},
		{Name: "PostLikesSet", Collection: redis.PostLikesSet, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityPostActive},
		{Name: "CommentLikesSet", Collection: redis.CommentLikesSet, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityCommentActive},
		{Name: "ConvMeta", Collection: redis.ConvMeta, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityConversation},
		{Name: "MessagesIndex", Collection: redis.MessagesIndex, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityConversation},
		{Name: "ConvWatermarks", Collection: redis.ConvWatermarks, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityConversation},
		{Name: "ConvMessageLedger", Collection: redis.ConvMessageLedger, Tier: redisOrphanTierCore, Entity: postgres.OrphanEntityConversation},

		// ── Vague 3 : Clés Tier 2 Object Pool à TTL borné ────────────────────
		// Posts utilise OrphanEntityPostPhysical pour préserver le Tombstone Pattern (visibility = -1).
		{Name: "Posts", Collection: redis.Posts, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityPostPhysical},
		{Name: "PostComments", Collection: redis.PostComments, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityPostActive},
		{Name: "Comments", Collection: redis.Comments, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityCommentActive},
		{Name: "Media", Collection: redis.Media, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityMediaActive},
		{Name: "Users", Collection: redis.Users, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityUser},
		{Name: "UserSettings", Collection: redis.UserSettings, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityUser},
		{Name: "UserTimeline", Collection: redis.UserTimeline, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityUser},
		{Name: "Saved", Collection: redis.Saved, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityUser},
		{Name: "Conversations", Collection: redis.Conversations, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityConversation},
		{Name: "Messages", Collection: redis.Messages, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityMessageActive},
		{Name: "MessageReactionCounts", Collection: redis.MessageReactionCounts, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityMessageActive},
		{Name: "MessageUserReactions", Collection: redis.MessageUserReactions, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityMessageActive},
		{Name: "POIReviews", Collection: redis.POIReviews, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityPOIReviewActive},
		{Name: "POIReviewsByPOI", Collection: redis.POIReviewsByPOI, Tier: redisOrphanTierObject, Entity: postgres.OrphanEntityPOIActive},
	}
}

// DefaultRedisOrphanCleanupConfig construit la configuration depuis les variables dynamiques.
func DefaultRedisOrphanCleanupConfig() RedisOrphanCleanupConfig {
	return RedisOrphanCleanupConfig{
		DryRun:            variables.RedisOrphanCleanerDryRun,
		GraceWindow:       variables.RedisOrphanGraceWindow,
		ScanCount:         int64(variables.RedisOrphanScanCount),
		PostgresBatchSize: variables.RedisOrphanPostgresBatchSize,
		UnlinkBatchSize:   variables.RedisOrphanUnlinkBatchSize,
		MaxKeysPerCycle:   variables.RedisOrphanMaxKeysPerCycle,
		Targets:           DefaultRedisOrphanTargets(),
		NowFn:             domain.NowTime,
		CheckExistingFn:   postgres.FuncFilterExistingEntityIDs,
		MetricsRegistry:   metrics.Default(),
	}
}

type orphanCandidate struct {
	key string
	id  int64
}

// processRedisOrphanCleanup exécute un cycle complet de détection (et d'UNLINK hors dry-run)
// des clés Redis orphelines sur les tiers Core et Object Pool.
func processRedisOrphanCleanup(ctx context.Context, cfg RedisOrphanCleanupConfig) (RedisOrphanCleanupSummary, error) {
	cfg = normalizeOrphanCleanupConfig(cfg)
	wallStart := time.Now()
	summary := RedisOrphanCleanupSummary{DryRun: cfg.DryRun}

	defer func() {
		summary.Duration = time.Since(wallStart)
		if summary.Duration < 0 {
			summary.Duration = 0
		}
		if cfg.MetricsRegistry != nil {
			cfg.MetricsRegistry.RecordRedisOrphanRunDuration(summary.Duration.Seconds())
		}
	}()

	if !redis.IsReady() && !redis.IsObjectPoolReady() {
		return summary, nil
	}

	for _, target := range cfg.Targets {
		if ctx.Err() != nil {
			return summary, ctx.Err()
		}
		if summary.KeysScanned >= int64(cfg.MaxKeysPerCycle) {
			summary.BudgetReached = true
			break
		}
		if target.Collection == nil || !target.Collection.IsReady() || target.Collection.Prefix == "" {
			continue
		}

		tStats, err := cleanOrphanTarget(ctx, cfg, target, &summary)
		if cfg.MetricsRegistry != nil && (tStats.scanned > 0 || tStats.skippedGrace > 0 || tStats.detected > 0 || tStats.unlinked > 0) {
			cfg.MetricsRegistry.RecordRedisOrphanStats(
				target.Tier,
				target.Entity,
				uint64(tStats.scanned),
				uint64(tStats.skippedGrace),
				uint64(tStats.detected),
				uint64(tStats.unlinked),
			)
		}

		if tStats.detected > 0 || tStats.unlinked > 0 || tStats.errors > 0 {
			numan_log.Info(ctx).
				Str("collection", target.Name).
				Str("tier", target.Tier).
				Str("entity", target.Entity).
				Bool("dry_run", cfg.DryRun).
				Int64("scanned", tStats.scanned).
				Int64("skipped_grace", tStats.skippedGrace).
				Int64("detected", tStats.detected).
				Int64("unlinked", tStats.unlinked).
				Int64("errors", tStats.errors).
				Msg("Redis Orphan Cleaner : bilan collection")
		}

		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return summary, err
			}
			// Une erreur SCAN sur une collection n'interrompt pas les suivantes
			continue
		}
	}

	numan_log.Info(ctx).
		Bool("dry_run", summary.DryRun).
		Int64("keys_scanned", summary.KeysScanned).
		Int64("skipped_grace", summary.SkippedGrace).
		Int64("skipped_invalid", summary.SkippedInvalid).
		Int64("orphans_detected", summary.OrphansDetected).
		Int64("orphans_unlinked", summary.OrphansUnlinked).
		Int64("errors", summary.Errors).
		Bool("budget_reached", summary.BudgetReached).
		Msg("Redis Orphan Cleaner : cycle terminé")

	return summary, nil
}

type targetCleanStats struct {
	scanned      int64
	skippedGrace int64
	detected     int64
	unlinked     int64
	errors       int64
}

func cleanOrphanTarget(
	ctx context.Context,
	cfg RedisOrphanCleanupConfig,
	target RedisOrphanTarget,
	summary *RedisOrphanCleanupSummary,
) (targetCleanStats, error) {
	var stats targetCleanStats
	pending := make([]orphanCandidate, 0, cfg.PostgresBatchSize)
	prefix := target.Collection.Prefix
	if !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}
	pattern := prefix + "*"

	flushBatch := func() error {
		if len(pending) == 0 {
			return nil
		}
		batch := pending
		pending = pending[:0]

		if err := ctx.Err(); err != nil {
			return err
		}

		// Déduplication des IDs Snowflake avant la requête Postgres
		uniqueIDs := make([]int64, 0, len(batch))
		seen := make(map[int64]struct{}, len(batch))
		for _, item := range batch {
			if _, ok := seen[item.id]; !ok {
				seen[item.id] = struct{}{}
				uniqueIDs = append(uniqueIDs, item.id)
			}
		}

		existing, errPg := cfg.CheckExistingFn(ctx, target.Entity, uniqueIDs)
		if errPg != nil {
			// CRITIQUE (Write-Behind Safety) : une erreur DB n'est JAMAIS interprétée comme une absence.
			stats.errors++
			summary.Errors++
			if cfg.MetricsRegistry != nil {
				cfg.MetricsRegistry.RecordRedisOrphanError("postgres")
			}
			numan_log.Error(ctx).Err(errPg).
				Str("collection", target.Name).
				Str("entity", target.Entity).
				Int("batch_size", len(uniqueIDs)).
				Msg("Redis Orphan Cleaner : échec vérification L3 Postgres — lot ignoré sans UNLINK")
			return nil
		}

		orphanKeys := make([]string, 0)
		for _, item := range batch {
			if !existing[item.id] {
				orphanKeys = append(orphanKeys, item.key)
			}
		}
		if len(orphanKeys) == 0 {
			return nil
		}

		stats.detected += int64(len(orphanKeys))
		summary.OrphansDetected += int64(len(orphanKeys))

		if cfg.DryRun {
			return nil
		}

		// Suppression non-bloquante par lots bornés via UNLINK Cluster-safe
		for start := 0; start < len(orphanKeys); start += cfg.UnlinkBatchSize {
			if err := ctx.Err(); err != nil {
				return err
			}
			end := start + cfg.UnlinkBatchSize
			if end > len(orphanKeys) {
				end = len(orphanKeys)
			}
			chunk := orphanKeys[start:end]

			var errUnlink error
			if target.Tier == redisOrphanTierObject {
				errUnlink = redis.UnlinkKeysOnObjects(ctx, chunk...)
			} else {
				errUnlink = redis.UnlinkKeysOnCore(ctx, chunk...)
			}
			if errUnlink != nil {
				stats.errors++
				summary.Errors++
				if cfg.MetricsRegistry != nil {
					cfg.MetricsRegistry.RecordRedisOrphanError("unlink")
				}
				numan_log.Error(ctx).Err(errUnlink).
					Str("collection", target.Name).
					Str("tier", target.Tier).
					Int("chunk_size", len(chunk)).
					Msg("Redis Orphan Cleaner : échec UNLINK Redis sur un lot")
				continue
			}

			stats.unlinked += int64(len(chunk))
			summary.OrphansUnlinked += int64(len(chunk))
		}
		return nil
	}

	scanFn := redis.ScanKeysBatchedOnCore
	if target.Tier == redisOrphanTierObject {
		scanFn = redis.ScanKeysBatchedOnObjects
	}

	errScan := scanFn(ctx, pattern, cfg.ScanCount, func(keys []string) error {
		now := cfg.NowFn()
		for _, fullKey := range keys {
			if summary.KeysScanned >= int64(cfg.MaxKeysPerCycle) {
				summary.BudgetReached = true
				return redis.ErrStopScan
			}
			summary.KeysScanned++
			stats.scanned++

			id, createdAt, eligible, inGrace := classifyOrphanCandidateKey(fullKey, prefix, now, cfg.GraceWindow)
			if !eligible {
				if inGrace {
					stats.skippedGrace++
					summary.SkippedGrace++
				} else {
					summary.SkippedInvalid++
				}
				continue
			}
			_ = createdAt

			pending = append(pending, orphanCandidate{key: fullKey, id: id})
			if len(pending) >= cfg.PostgresBatchSize {
				if err := flushBatch(); err != nil {
					return err
				}
			}
		}

		if cfg.OnProgress != nil {
			if err := cfg.OnProgress(summary.KeysScanned); err != nil {
				return err
			}
		}
		return nil
	})

	if errScan != nil {
		if errors.Is(errScan, context.Canceled) || errors.Is(errScan, context.DeadlineExceeded) {
			return stats, errScan
		}
		stats.errors++
		summary.Errors++
		if cfg.MetricsRegistry != nil {
			cfg.MetricsRegistry.RecordRedisOrphanError("scan")
		}
		numan_log.Warn(ctx).Err(errScan).
			Str("collection", target.Name).
			Str("tier", target.Tier).
			Msg("Redis Orphan Cleaner : erreur SCAN Redis — aucun UNLINK sur cette page")
		return stats, errScan
	}

	// Vidage du dernier lot partiel accumulé (uniquement si le SCAN s'est déroulé sans erreur)
	if errFlush := flushBatch(); errFlush != nil {
		return stats, errFlush
	}

	return stats, nil
}

// classifyOrphanCandidateKey extrait strictement un ID Snowflake numérique unique
// du suffixe d'une clé Redis et applique la fenêtre de grâce Write-Behind.
// Retourne (id, createdAt, eligible, skippedDueToGraceWindow).
func classifyOrphanCandidateKey(fullKey, prefix string, now time.Time, graceWindow time.Duration) (int64, time.Time, bool, bool) {
	if !strings.HasPrefix(fullKey, prefix) {
		return 0, time.Time{}, false, false
	}
	suffix := strings.TrimPrefix(fullKey, prefix)
	if suffix == "" || strings.Contains(suffix, ":") {
		return 0, time.Time{}, false, false
	}
	id, err := strconv.ParseInt(suffix, 10, 64)
	if err != nil || id <= 0 {
		return 0, time.Time{}, false, false
	}
	createdAt, ok := pkg.ExtractSnowflakeTime(id)
	if !ok {
		return 0, time.Time{}, false, false
	}
	// Protection contre le clock skew (timestamp dans le futur) et la fenêtre de grâce Write-Behind
	if createdAt.After(now) || now.Sub(createdAt) < graceWindow {
		return id, createdAt, false, true
	}
	return id, createdAt, true, false
}

func normalizeOrphanCleanupConfig(cfg RedisOrphanCleanupConfig) RedisOrphanCleanupConfig {
	if cfg.GraceWindow <= 0 {
		cfg.GraceWindow = 15 * time.Minute
	}
	if cfg.ScanCount <= 0 {
		cfg.ScanCount = 200
	}
	if cfg.PostgresBatchSize <= 0 {
		cfg.PostgresBatchSize = 500
	}
	if cfg.UnlinkBatchSize <= 0 {
		cfg.UnlinkBatchSize = 200
	}
	if cfg.MaxKeysPerCycle <= 0 {
		cfg.MaxKeysPerCycle = 50000
	}
	if len(cfg.Targets) == 0 {
		cfg.Targets = DefaultRedisOrphanTargets()
	}
	if cfg.NowFn == nil {
		cfg.NowFn = domain.NowTime
	}
	if cfg.CheckExistingFn == nil {
		cfg.CheckExistingFn = postgres.FuncFilterExistingEntityIDs
	}
	return cfg
}
