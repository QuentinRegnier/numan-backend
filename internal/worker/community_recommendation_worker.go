package worker

import (
	"context"
	"sort"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	pgRepo "github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/community_recommendation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # WORKER & CRON : SYSTÈME DE RECOMMANDATION DE COMMUNAUTÉS PUBLIQUES
// ############################################################################

// CommunityRecoCronStats regroupe les métriques d'exécution d'un cycle du Cron
// de calcul global des classements de communautés publiques.
type CommunityRecoCronStats struct {
	LanguagesProcessed   int
	CommunitiesProcessed int
	SkippedLocked        bool
	ErrorsCount          int
	DurationMs           int64
}

func resolveCommunityRecoCronInterval() time.Duration {
	interval := variables.CommunityRecoCronInterval
	if interval < variables.CommunityRecoMinCronInterval {
		interval = variables.CommunityRecoMinCronInterval
	}
	if interval > variables.CommunityRecoMaxCronInterval {
		interval = variables.CommunityRecoMaxCronInterval
	}
	return interval
}

// startCommunityRecommendationCron lance la tâche planifiée (par défaut toutes les 2h)
// qui recalcule les scores globaux S_global de toutes les communautés publiques actives
// et remplace atomiquement les ZSETs Redis community:reco:ranked:{lang}.
func startCommunityRecommendationCron(ctx context.Context) {
	numan_log.Info(ctx).
		Str("interval", resolveCommunityRecoCronInterval().String()).
		Msg("Démarrage du Cron de calcul global des recommandations de communautés publiques...")

	go func() {
		timer := time.NewTimer(resolveCommunityRecoCronInterval())
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				numan_log.Info(ctx).Msg("Arrêt gracieux du Cron de recommandation de communautés publiques.")
				return
			case <-timer.C:
				_, _ = RunCommunityRecommendationCronCycle(ctx)
				timer.Reset(resolveCommunityRecoCronInterval())
			}
		}
	}()
}

// RunCommunityRecommendationCronCycle exécute un cycle complet de recalcul des scores
// globaux de toutes les communautés publiques actives, protégé par verrou distribué Redis.
func RunCommunityRecommendationCronCycle(ctx context.Context) (CommunityRecoCronStats, error) {
	startTime := time.Now()
	stats := CommunityRecoCronStats{}

	if err := ctx.Err(); err != nil {
		return stats, err
	}

	// ── ÉTAPE 1 : VERROU DISTRIBUÉ REDIS (EXCLUSION MUTUELLE MULTI-INSTANCES) ─
	lockToken, acquired, errLock := redis.AcquireCronLock(ctx, variables.CommunityRecoCronLockKey, variables.CommunityRecoCronLockTTL)
	if errLock != nil {
		numan_log.Error(ctx).Err(errLock).Msg("CommunityRecoCron : échec d'acquisition du verrou distribué Redis")
		stats.ErrorsCount++
		stats.DurationMs = time.Since(startTime).Milliseconds()
		return stats, errLock
	}
	if !acquired && lockToken == "" && redis.CronLocks != nil && redis.CronLocks.Client != nil {
		numan_log.Info(ctx).Msg("CommunityRecoCron : cycle ignoré (verrou déjà détenu par une autre instance)")
		stats.SkippedLocked = true
		stats.DurationMs = time.Since(startTime).Milliseconds()
		return stats, nil
	}
	if lockToken != "" {
		defer func() {
			if errRel := redis.ReleaseCronLock(context.Background(), variables.CommunityRecoCronLockKey, lockToken); errRel != nil {
				numan_log.Warn(ctx).Err(errRel).Msg("CommunityRecoCron : échec de libération du verrou distribué")
			}
		}()
	}

	numan_log.Info(ctx).Msg("CommunityRecoCron : démarrage du cycle de calcul global")

	// ── ÉTAPE 2 : CHARGEMENT DES COMMUNAUTÉS PUBLIQUES ACTIVES (L3) ─────────
	activeCommunities, errLoad := pgRepo.FuncLoadActiveCommunitiesForReco(ctx)
	if errLoad != nil {
		numan_log.Error(ctx).Err(errLoad).Msg("CommunityRecoCron : échec de chargement des communautés actives depuis PostgreSQL")
		stats.ErrorsCount++
		stats.DurationMs = time.Since(startTime).Milliseconds()
		return stats, errLoad
	}

	byLang := make(map[string][]community_recommendation_models.ActiveCommunityRecoRecord)
	for _, comm := range activeCommunities {
		normLang, ok := variables.NormalizeCommunityLang(comm.Lang)
		if !ok {
			numan_log.Warn(ctx).
				Int64("conv_id", comm.ID).
				Str("raw_lang", comm.Lang).
				Msg("CommunityRecoCron : communauté ignorée (langue non supportée ou vide)")
			continue
		}
		comm.Lang = normLang
		byLang[normLang] = append(byLang[normLang], comm)
	}

	// On parcourt toutes les langues supportées (pour purger les ZSETs devenus vides)
	langs := variables.SupportedLanguageISOs()
	sort.Strings(langs)

	now := domain.NowTime().UTC()
	dates := cache_service.CommunityRecoTrailingDates(now, 7)
	cfg := community_recommendation_service.LoadScoringConfig()

	// ── ÉTAPE 3 : CALCUL ET REMPLACEMENT ATOMIQUE PAR LANGUE ────────────────
	for _, lang := range langs {
		if err := ctx.Err(); err != nil {
			stats.DurationMs = time.Since(startTime).Milliseconds()
			return stats, err
		}

		langComms := byLang[lang]
		if len(langComms) == 0 {
			if errClear := cache_service.ReplaceRankedCommunitiesByLang(ctx, lang, nil); errClear != nil {
				numan_log.Warn(ctx).Err(errClear).Str("lang", lang).Msg("CommunityRecoCron : échec de purge du ZSET vide")
				stats.ErrorsCount++
				continue
			}
			stats.LanguagesProcessed++
			continue
		}

		processedCount, errLang := processCommunityRecoLanguage(ctx, lang, langComms, dates, now, cfg)
		if errLang != nil {
			numan_log.Error(ctx).
				Err(errLang).
				Str("lang", lang).
				Int("communities_count", len(langComms)).
				Msg("CommunityRecoCron : échec de traitement pour la langue (conservation du dernier classement valide)")
			stats.ErrorsCount++
			continue
		}

		stats.LanguagesProcessed++
		stats.CommunitiesProcessed += processedCount
	}

	stats.DurationMs = time.Since(startTime).Milliseconds()
	numan_log.Info(ctx).
		Int("languages_processed", stats.LanguagesProcessed).
		Int("communities_processed", stats.CommunitiesProcessed).
		Int("errors_count", stats.ErrorsCount).
		Int64("duration_ms", stats.DurationMs).
		Msg("CommunityRecoCron : cycle de calcul global terminé")

	return stats, nil
}

type communityTrustMembers struct {
	ownerID   int64
	adminIDs  []int64
	memberIDs []int64
}

func processCommunityRecoLanguage(
	ctx context.Context,
	lang string,
	comms []community_recommendation_models.ActiveCommunityRecoRecord,
	dates []string,
	now time.Time,
	cfg community_recommendation_models.ScoringConfig,
) (int, error) {
	convIDs := make([]int64, len(comms))
	for i, c := range comms {
		convIDs[i] = c.ID
	}

	// 1. Lecture batch télémétrie 7 jours depuis Redis L1 (dégradation gracieuse à 0 en cas d'erreur)
	msgCounts, errMsg := cache_service.GetCommunity7DayMessageCounts(ctx, convIDs, dates)
	if errMsg != nil {
		numan_log.Warn(ctx).Err(errMsg).Str("lang", lang).Msg("CommunityRecoCron : échec lecture msg_count 7j, dégradation à 0")
		msgCounts = map[int64]int64{}
	}

	growthCounts, errGrowth := cache_service.GetCommunity7DayGrowthCounts(ctx, convIDs, dates)
	if errGrowth != nil {
		numan_log.Warn(ctx).Err(errGrowth).Str("lang", lang).Msg("CommunityRecoCron : échec lecture growth 7j, dégradation à 0")
		growthCounts = map[int64]int64{}
	}

	authorCounts, errAuthors := cache_service.GetCommunity7DayUniqueAuthors(ctx, convIDs, dates)
	if errAuthors != nil {
		numan_log.Warn(ctx).Err(errAuthors).Str("lang", lang).Msg("CommunityRecoCron : échec lecture authors HLL 7j, dégradation à 0")
		authorCounts = map[int64]int64{}
	}

	// 2. Lecture batch des rôles membres depuis PostgreSQL L3 (plafonnée par TrustMemberSampleCap)
	memberRecords, errMembers := pgRepo.FuncLoadCommunityMembersForTrust(ctx, convIDs, cfg.TrustMemberSampleCap)
	if errMembers != nil {
		return 0, errMembers
	}

	byConv := make(map[int64]communityTrustMembers, len(convIDs))
	uniqueUserSet := make(map[int64]struct{}, len(memberRecords))
	for _, rec := range memberRecords {
		if rec.UserID <= 0 {
			continue
		}
		uniqueUserSet[rec.UserID] = struct{}{}

		bucket := byConv[rec.ConversationID]
		switch rec.Role {
		case variables.MemberRoleOwner:
			bucket.ownerID = rec.UserID
		case variables.MemberRoleAdmin:
			bucket.adminIDs = append(bucket.adminIDs, rec.UserID)
		case variables.MemberRoleNormal:
			bucket.memberIDs = append(bucket.memberIDs, rec.UserID)
		}
		byConv[rec.ConversationID] = bucket
	}

	// 3. Lecture batch UsersLite (MGET chunké) pour résoudre les TrustScores sans N+1
	uniqueUserIDs := make([]int64, 0, len(uniqueUserSet))
	for uid := range uniqueUserSet {
		uniqueUserIDs = append(uniqueUserIDs, uid)
	}

	usersLiteMap, errUsers := cache_service.GetUsersLiteBatch(ctx, uniqueUserIDs)
	if errUsers != nil {
		numan_log.Warn(ctx).Err(errUsers).Str("lang", lang).Msg("CommunityRecoCron : échec lecture UsersLite batch, repli sur TrustPriorMean")
		usersLiteMap = nil
	}

	resolveTrust := func(userID int64) float64 {
		if usersLiteMap != nil {
			if u, ok := usersLiteMap[userID]; ok && u.ID > 0 && u.TrustScore >= 0 {
				return u.TrustScore
			}
		}
		return cfg.TrustPriorMean
	}

	// 4. Calcul des scores globaux S_global
	entries := make([]cache_service.RankedCommunityEntry, 0, len(comms))
	for _, comm := range comms {
		hasOwnerTrust := false
		ownerTrust := 0.0
		var adminTrusts []float64
		var memberTrusts []float64

		if bucket, ok := byConv[comm.ID]; ok {
			if bucket.ownerID > 0 {
				hasOwnerTrust = true
				ownerTrust = resolveTrust(bucket.ownerID)
			}
			if len(bucket.adminIDs) > 0 {
				adminTrusts = make([]float64, len(bucket.adminIDs))
				for i, uid := range bucket.adminIDs {
					adminTrusts[i] = resolveTrust(uid)
				}
			}
			if len(bucket.memberIDs) > 0 {
				memberTrusts = make([]float64, len(bucket.memberIDs))
				for i, uid := range bucket.memberIDs {
					memberTrusts[i] = resolveTrust(uid)
				}
			}
		}

		score := community_recommendation_service.ComputeGlobalScore(
			community_recommendation_models.CommunityScoreInput{
				ConversationID:  comm.ID,
				MemberCount:     comm.MemberCount,
				MsgCount7d:      msgCounts[comm.ID],
				UniqueAuthors7d: authorCounts[comm.ID],
				Growth7d:        growthCounts[comm.ID],
				HasOwnerTrust:   hasOwnerTrust,
				OwnerTrust:      ownerTrust,
				AdminTrusts:     adminTrusts,
				MemberTrusts:    memberTrusts,
				Mode:            models.ResolveCommunityModeFromWritePermission(comm.Settings.WritePermission),
				IsPartner:       comm.IsPartner,
				CreatedAt:       time.UnixMilli(comm.CreatedAt).UTC(),
				Now:             now,
			},
			cfg,
		)

		entries = append(entries, cache_service.RankedCommunityEntry{
			ConversationID: comm.ID,
			Score:          score,
		})
	}

	// 5. Remplacement atomique du ZSET Redis community:reco:ranked:{lang}
	if errReplace := cache_service.ReplaceRankedCommunitiesByLang(ctx, lang, entries); errReplace != nil {
		return 0, errReplace
	}

	return len(entries), nil
}
