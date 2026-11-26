package algorithm_service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # LE MAGASINIER : COLLECTE ET EXPANSION SÉMANTIQUE (PAR LANGUE)
// ############################################################################

// collectCandidates construit les 3 paniers (A, B, C) avec leurs ADN respectifs dans la langue cible.
func collectCandidates(ctx context.Context, userID int64, seeds [3]int64, quotas Quotas, lang ...int) (*feedBaskets, error) {
	if err := quotas.validate(); err != nil {
		return nil, err
	}

	resolvedLang := variables.DefaultContentLanguage
	if len(lang) > 0 {
		resolvedLang = variables.NormalizeLangForCache(lang[0])
	}

	baskets := newFeedBaskets(quotas.MaxCandidates, seeds[0], seeds[1], seeds[2])

	// ─────────────────────────────────────────────────────────────────────────────
	// ÉTAPE 1 : Le Socle Social (Boîte aux lettres)
	// ─────────────────────────────────────────────────────────────────────────────
	err := baskets.loadSocialMailbox(ctx, userID)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("collectCandidates: Échec du chargement de la boîte aux lettres")
		return nil, err
	}

	// ─────────────────────────────────────────────────────────────────────────────
	// ÉTAPE 2 : Fusion Télémétrie / Graph 1-Hop / Leaderboard par Langue
	// ─────────────────────────────────────────────────────────────────────────────
	semanticTagCloud := buildTagCloud(ctx, userID, resolvedLang)

	// ──────────────────────────────────────────────────────────────────────────────
	// ÉTAPE 3 : Remplissage Déterministe des 3 Paniers
	// ─────────────────────────────────────────────────────────────────────────────
	fillBasket(ctx, userID, baskets.FeedA, quotas, semanticTagCloud, resolvedLang)
	fillBasket(ctx, userID, baskets.FeedB, quotas, semanticTagCloud, resolvedLang)
	fillBasket(ctx, userID, baskets.FeedC, quotas, semanticTagCloud, resolvedLang)

	return baskets, nil
}

// collectSingleBasket construit un unique panier avec son ADN strict (Cas d'Extension).
// Utilise la Seed du flux actif pour garantir la continuité de l'identité algorithmique.
func collectSingleBasket(ctx context.Context, userID int64, activeSeed int64, quotas Quotas, lang ...int) (*candidateBasket, error) {
	if err := quotas.validate(); err != nil {
		return nil, err
	}

	resolvedLang := variables.DefaultContentLanguage
	if len(lang) > 0 {
		resolvedLang = variables.NormalizeLangForCache(lang[0])
	}

	baskets := newFeedBaskets(quotas.MaxCandidates, activeSeed, activeSeed, activeSeed)
	err := baskets.loadSocialMailbox(ctx, userID)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("collectSingleBasket: Échec du chargement de la boîte aux lettres")
		return nil, err
	}

	singleBasket := baskets.FeedA
	semanticTagCloud := buildTagCloud(ctx, userID, resolvedLang)

	fillBasket(ctx, userID, singleBasket, quotas, semanticTagCloud, resolvedLang)

	return singleBasket, nil
}

// readHashtagLeaderboard lit le leaderboard de hashtags pour une langue avec fallback sur les clés legacy.
func readHashtagLeaderboard(ctx context.Context, lang int, stop int64) ([]redis.ZMember, error) {
	normLang := variables.NormalizeLangForCache(lang)
	if redis.HashtagLeaderboard.IsReady() {
		if data, err := redis.HashtagLeaderboard.ZRevRangeWithScores(ctx, variables.HashtagLeaderboardSubKey(normLang), 0, stop); err == nil && len(data) > 0 {
			return data, nil
		}
		if data, err := redis.HashtagLeaderboard.ZRevRangeWithScores(ctx, "global", 0, stop); err == nil && len(data) > 0 {
			return data, nil
		}
	}
	if redis.Tags.IsReady() {
		if data, err := redis.Tags.ZRevRangeWithScores(ctx, "trending", 0, stop); err == nil && len(data) > 0 {
			return data, nil
		}
	}
	return redis.ZRevRangeWithScores(ctx, variables.RedisKeyHashtagLeaderboard, 0, stop)
}

// ############################################################################
// # CRÉATION DU NUAGE SÉMANTIQUE (L'Effet Pingouin)
// ############################################################################

// buildTagCloud abstrait la création du Super-Nuage sémantique pour éviter la duplication.
// Interroge la télémétrie, le graphe 1-Hop par langue et le leaderboard de la langue.
func buildTagCloud(ctx context.Context, userID int64, lang ...int) map[string]float64 {
	resolvedLang := variables.DefaultContentLanguage
	if len(lang) > 0 {
		resolvedLang = variables.NormalizeLangForCache(lang[0])
	}

	userTelemetryMap := make(map[string]float64)

	// 1. Lecture de la Télémétrie Personnelle (L1 Speed Cache)
	profile, err := cache_service.GetTelemetryProfile(ctx, userID)
	topPersonalTags := profile.TopTags

	if err == nil && len(topPersonalTags) > 0 {
		var currentWeight = 1.0
		for _, rawTag := range topPersonalTags {
			tag := service.ResolveCanonicalTag(ctx, rawTag, resolvedLang)
			if tag == "" {
				tag = rawTag
			}
			userTelemetryMap[tag] = currentWeight
			currentWeight -= 0.1
			if currentWeight < 0.1 {
				currentWeight = 0.1
			}
		}
	} else {
		// FALLBACK : Si le profil est vierge (Nouvel Utilisateur), on le branche sur le Top de sa langue
		leaderboardData, errLeaderboard := readHashtagLeaderboard(ctx, resolvedLang, 4)
		if errLeaderboard == nil && len(leaderboardData) > 0 {
			var currentWeight = 1.0
			for _, zData := range leaderboardData {
				if tagName, ok := zData.Member.(string); ok && tagName != "" {
					userTelemetryMap[tagName] = currentWeight
					currentWeight -= 0.1
				}
			}
		} else {
			userTelemetryMap["bienvenue"] = 1.0 // Sécurité absolue
		}
	}

	// 1.5 OPTIMISATION "JUSTIN BIEBER" : Injection des auteurs SUIVIS PAR l'utilisateur (out:1 / out:2)
	if followedIDs, errRel := cache_service.GetSpeedFollowingIndex(ctx, userID); errRel == nil && len(followedIDs) > 0 {
		for _, followedID := range followedIDs {
			vipTag := fmt.Sprintf("user_%d", followedID)
			userTelemetryMap[vipTag] = 1.0 // Poids maximum pour les créateurs choisis
		}
	}

	// 2. Lecture du Leaderboard de la langue pour le calcul des Multiplicateurs de Viralité
	leaderboardData, err := readHashtagLeaderboard(ctx, resolvedLang, 49)
	if err != nil {
		numan_log.Warn(ctx).Err(err).Int("lang", resolvedLang).Msg("buildTagCloud: Échec de la lecture du leaderboard")
	}
	leaderboardBoostsMap := make(map[string]float64)

	if len(leaderboardData) > 0 {
		maxGlobalScore := leaderboardData[0].Score
		for _, zData := range leaderboardData {
			if maxGlobalScore > 0 {
				if tagName, ok := zData.Member.(string); ok && tagName != "" {
					// Le boost de viralité va de 1.0 à 1.5 selon le score du tag vs le Tag #1 de la langue
					leaderboardBoostsMap[tagName] = 1.0 + (zData.Score / maxGlobalScore * 0.5)
				}
			}
		}
	}

	// 3. Expansion Sémantique (Graphe 1-Hop par langue) et Assemblage du Super-Nuage
	finalTagCloud := make(map[string]float64)

	for coreTag, personalAffinity := range userTelemetryMap {
		var tagBoost = 1.0
		if val, exists := leaderboardBoostsMap[coreTag]; exists {
			tagBoost = val
		}

		finalTagCloud[coreTag] += personalAffinity * 1.0 * tagBoost

		// Gain CPU : On ne fait pas d'expansion de graphe sur les tags d'auteurs
		if len(coreTag) > 5 && coreTag[:5] == "user_" {
			continue
		}

		// Recherche des "cousins sémantiques" (1-Hop) via le Graph Cache dans la langue cible
		neighborTags := cache_service.GetRelatedTagsLazy(ctx, coreTag, resolvedLang)
		for neighborTag, edgeWeight := range neighborTags {
			var neighborBoost = 1.0
			if val, exists := leaderboardBoostsMap[neighborTag]; exists {
				neighborBoost = val
			}
			finalTagCloud[neighborTag] += personalAffinity * edgeWeight * neighborBoost
		}
	}

	return finalTagCloud
}

// ############################################################################
// # LE REMPLISSAGE (Gestion du Quota, ADN du Panier & Cascade Cold-Start)
// ############################################################################

// fillBasket remplit un panier spécifique en respectant les quotas, l'ADN du panier
// (TagVariance, ExplorationExponent, FreshnessBias) et la cascade Cold-Start par langue.
func fillBasket(ctx context.Context, userID int64, basket *candidateBasket, quotas Quotas, initialTagCloud map[string]float64, lang ...int) {
	resolvedLang := variables.DefaultContentLanguage
	if len(lang) > 0 {
		resolvedLang = variables.NormalizeLangForCache(lang[0])
	}

	// Application de TagVariance de l'ADN du panier (±15%) entre TagRatio et GlobalRatio
	effectiveTagRatio := quotas.TagRatio + basket.Personality.TagVariance
	effectiveGlobalRatio := quotas.GlobalRatio - basket.Personality.TagVariance
	if effectiveTagRatio < 0.05 {
		effectiveTagRatio = 0.05
	}
	if effectiveGlobalRatio < 0.05 {
		effectiveGlobalRatio = 0.05
	}

	globalTargetQuota := int(math.Round(float64(quotas.MaxCandidates) * effectiveGlobalRatio))
	tagTargetQuota := int(math.Round(float64(quotas.MaxCandidates) * effectiveTagRatio))
	if globalTargetQuota < 0 {
		globalTargetQuota = 0
	}
	if tagTargetQuota < 0 {
		tagTargetQuota = 0
	}

	now := domain.NowTime().UTC()
	currentDateStr := now.Format("20060102")
	yesterdayDateStr := now.Add(-24 * time.Hour).Format("20060102")
	currentHourStr := now.Format("2006010215")
	year, isoWeek := now.ISOWeek()
	currentWeekStr := fmt.Sprintf("%d-W%02d", year, isoWeek)

	// ─────────────────────────────────────────────────────────────────────────────
	// PHASE 1 : COLLECTE GLOBALE (SÉRENDIPITÉ PURE — HOURLY / DAILY PAR LANGUE)
	// ─────────────────────────────────────────────────────────────────────────────
	successfullyAddedGlobally := fetchGlobalCandidatesForLang(ctx, userID, basket, resolvedLang, currentHourStr, currentDateStr, yesterdayDateStr, globalTargetQuota)

	// Gestion du déficit : si on a épuisé le ZSET global, on reporte la charge sur les tags
	if successfullyAddedGlobally < globalTargetQuota {
		deficit := globalTargetQuota - successfullyAddedGlobally
		tagTargetQuota += deficit
	}

	// ─────────────────────────────────────────────────────────────────────────────
	// PHASE 2 : COLLECTE CIBLÉE (NUAGE DE TAGS PAR LANGUE : DAILY -> WEEKLY)
	// ─────────────────────────────────────────────────────────────────────────────
	workingTagCloud := make(map[string]float64, len(initialTagCloud))
	exp := basket.Personality.ExplorationExponent
	if exp <= 0 {
		exp = 1.0
	}
	for tag, weight := range initialTagCloud {
		workingTagCloud[tag] = math.Pow(weight, exp)
	}

	successfullyAddedFromTags := 0
	currentGraphDepth := 1
	const maxGraphDepth = 2 // 1 = Nuage initial, 2 = Cousins directs (1-Hop). On bloque ensuite.

	for successfullyAddedFromTags < tagTargetQuota && currentGraphDepth <= maxGraphDepth && len(workingTagCloud) > 0 {
		var totalCloudWeight = 0.0
		for _, weight := range workingTagCloud {
			totalCloudWeight += weight
		}
		if totalCloudWeight <= 0 {
			break
		}

		for tag, weight := range workingTagCloud {
			if successfullyAddedFromTags >= tagTargetQuota {
				break
			}

			targetForThisTag := int(math.Ceil(float64(tagTargetQuota-successfullyAddedFromTags) * (weight / totalCloudWeight)))
			if targetForThisTag <= 0 {
				targetForThisTag = 1
			}

			addedCount := fetchTagCandidatesForLang(ctx, userID, basket, resolvedLang, tag, currentDateStr, yesterdayDateStr, currentWeekStr, targetForThisTag)
			successfullyAddedFromTags += addedCount
		}

		if successfullyAddedFromTags >= tagTargetQuota {
			break
		}

		// EXPANSION DYNAMIQUE (1-Hop dans la langue cible)
		newDiscoveredTags := make(map[string]float64)
		for tag, weight := range workingTagCloud {
			if len(tag) > 5 && tag[:5] == "user_" {
				continue
			}
			neighborTags := cache_service.GetRelatedTagsLazy(ctx, tag, resolvedLang)
			for neighborTag, edgeWeight := range neighborTags {
				if _, alreadyExists := workingTagCloud[neighborTag]; !alreadyExists {
					newDiscoveredTags[neighborTag] = weight * edgeWeight * 0.8
				}
			}
		}

		if len(newDiscoveredTags) == 0 || currentGraphDepth >= maxGraphDepth {
			break
		}

		for tag, weight := range newDiscoveredTags {
			workingTagCloud[tag] = weight
		}
		currentGraphDepth++
	}

	// ─────────────────────────────────────────────────────────────────────────────
	// PHASE 3 : CASCADE COLD-START & FALLBACK INTER-LANGUES (DÉFICIT DE L'ÉLÉPHANT)
	// ─────────────────────────────────────────────────────────────────────────────
	// Séquence déterministe :
	// 1. Même langue : TrendGlobalDaily (aujourd'hui / hier)
	// 2. Même langue : RankedPosts strict:recent:{lang}
	// 3. Langues de repli configurées (variables.GetLanguageFallbackChain) : TrendGlobalDaily puis strict:recent
	if successfullyAddedFromTags < tagTargetQuota {
		deficit := tagTargetQuota - successfullyAddedFromTags
		added := fetchGlobalCandidatesForLang(ctx, userID, basket, resolvedLang, currentHourStr, currentDateStr, yesterdayDateStr, deficit)
		deficit -= added

		if deficit > 0 {
			addedRecent := fetchRecentFallbackForLang(ctx, userID, basket, resolvedLang, deficit)
			deficit -= addedRecent
		}

		if deficit > 0 {
			fallbackChain := variables.GetLanguageFallbackChain(resolvedLang)
			for idx, fbLang := range fallbackChain {
				if idx == 0 || fbLang == resolvedLang || deficit <= 0 {
					continue
				}
				addedFb := fetchGlobalCandidatesForLang(ctx, userID, basket, fbLang, currentHourStr, currentDateStr, yesterdayDateStr, deficit)
				deficit -= addedFb
				if deficit > 0 {
					addedFbRecent := fetchRecentFallbackForLang(ctx, userID, basket, fbLang, deficit)
					deficit -= addedFbRecent
				}
			}
		}
	}
}

// fetchGlobalCandidatesForLang pioche dans les ZSETs globaux (Hourly si FreshnessBias l'active, puis Daily aujourd'hui/hier, puis clés legacy).
func fetchGlobalCandidatesForLang(ctx context.Context, userID int64, basket *candidateBasket, lang int, hourStr, dateStr, yesterdayStr string, target int) int {
	if target <= 0 {
		return 0
	}
	added := 0

	// Biais de fraîcheur : si le tirage est inférieur à FreshnessBias, on pioche jusqu'à 50% dans le rayon Horaire
	if basket.Personality.FreshnessBias > 0.5 && redis.TrendGlobalHourly != nil {
		hourlyQuota := target / 2
		if hourlyQuota > 0 {
			hourlyKey := redis.TrendGlobalHourly.Key(variables.TrendGlobalHourlySubKey(lang, hourStr))
			added += basket.fetchDeterministicallyFromZSET(ctx, userID, hourlyKey, hourlyQuota, originGlobal)
		}
	}

	if added < target && redis.TrendGlobalDaily != nil {
		dailyKey := redis.TrendGlobalDaily.Key(variables.TrendGlobalDailySubKey(lang, dateStr))
		added += basket.fetchDeterministicallyFromZSET(ctx, userID, dailyKey, target-added, originGlobal)
	}
	if added < target && yesterdayStr != "" && redis.TrendGlobalDaily != nil {
		yesterdayKey := redis.TrendGlobalDaily.Key(variables.TrendGlobalDailySubKey(lang, yesterdayStr))
		added += basket.fetchDeterministicallyFromZSET(ctx, userID, yesterdayKey, target-added, originGlobal)
	}
	if added < target && lang == variables.DefaultContentLanguage {
		// Fallback clés legacy pour compatibilité ascendante avec les tests existants
		if redis.TrendGlobalDaily != nil {
			added += basket.fetchDeterministicallyFromZSET(ctx, userID, redis.TrendGlobalDaily.Key(dateStr), target-added, originGlobal)
		}
		if added < target {
			legacyGlobalKey := fmt.Sprintf(variables.RedisKeyTrendGlobalDaily, dateStr)
			added += basket.fetchDeterministicallyFromZSET(ctx, userID, legacyGlobalKey, target-added, originGlobal)
		}
	}
	return added
}

// fetchTagCandidatesForLang pioche dans TrendTagDaily (aujourd'hui/hier) puis TrendTagWeekly puis TagPosts de la langue cible.
func fetchTagCandidatesForLang(ctx context.Context, userID int64, basket *candidateBasket, lang int, tag, dateStr, yesterdayStr, weekStr string, target int) int {
	if target <= 0 {
		return 0
	}
	added := 0

	if redis.TrendTagDaily != nil {
		dailyTagKey := redis.TrendTagDaily.Key(variables.TrendTagDailySubKey(lang, tag, dateStr))
		added += basket.fetchDeterministicallyFromZSET(ctx, userID, dailyTagKey, target-added, originTag)
		if added < target && yesterdayStr != "" {
			yTagKey := redis.TrendTagDaily.Key(variables.TrendTagDailySubKey(lang, tag, yesterdayStr))
			added += basket.fetchDeterministicallyFromZSET(ctx, userID, yTagKey, target-added, originTag)
		}
	}

	// Fallback Cold-Start 1 : Rayon Hebdomadaire (Weekly) de la même langue
	if added < target && redis.TrendTagWeekly != nil {
		weeklyTagKey := redis.TrendTagWeekly.Key(variables.TrendTagWeeklySubKey(lang, tag, weekStr))
		added += basket.fetchDeterministicallyFromZSET(ctx, userID, weeklyTagKey, target-added, originTag)
	}

	// Fallback Cold-Start 2 : Index chronologique du tag dans la même langue
	if added < target && redis.TagPosts != nil {
		idxTagKey := redis.TagPosts.Key(variables.TagIndexSubKey(lang, tag))
		added += basket.fetchDeterministicallyFromZSET(ctx, userID, idxTagKey, target-added, originTag)
	}

	// Fallback compatibilité ascendante (clés sans préfixe langue / legacy)
	if added < target && lang == variables.DefaultContentLanguage {
		if redis.TrendTagDaily != nil {
			added += basket.fetchDeterministicallyFromZSET(ctx, userID, redis.TrendTagDaily.Key(fmt.Sprintf("%s:%s", tag, dateStr)), target-added, originTag)
		}
		if added < target {
			legacyTagKey := fmt.Sprintf(variables.RedisKeyTrendTagDaily, tag, dateStr)
			added += basket.fetchDeterministicallyFromZSET(ctx, userID, legacyTagKey, target-added, originTag)
		}
	}
	return added
}

// fetchRecentFallbackForLang pioche dans RankedPosts strict:recent:{lang} lors d'un Cold-Start.
func fetchRecentFallbackForLang(ctx context.Context, userID int64, basket *candidateBasket, lang int, target int) int {
	if target <= 0 || redis.RankedPosts == nil {
		return 0
	}
	recentKey := redis.RankedPosts.Key(variables.StrictRankSubKey(variables.StrictTypeRecent, lang))
	added := basket.fetchDeterministicallyFromZSET(ctx, userID, recentKey, target, originGlobal)
	if added < target && lang == variables.DefaultContentLanguage {
		added += basket.fetchDeterministicallyFromZSET(ctx, userID, variables.RedisKeyStrictRecent, target-added, originGlobal)
	}
	return added
}
