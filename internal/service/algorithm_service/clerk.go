package algorithm_service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # LE MAGASINIER : COLLECTE ET EXPANSION SÉMANTIQUE
// ############################################################################

// collectCandidates construit les 3 paniers (A, B, C) avec leurs ADN respectifs.
func collectCandidates(ctx context.Context, userID int64, seeds [3]int64, quotas Quotas) (*feedBaskets, error) {
	if err := quotas.validate(); err != nil {
		return nil, err // Utilise l'AppError générée par la validation
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
	// ÉTAPE 2 : Fusion Télémétrie / Graph 1-Hop / Leaderboard Mondial
	// ─────────────────────────────────────────────────────────────────────────────
	semanticTagCloud := buildTagCloud(ctx, userID)

	// ──────────────────────────────────────────────────────────────────────────────
	// ÉTAPE 3 : Remplissage Déterministe des 3 Paniers
	// ─────────────────────────────────────────────────────────────────────────────
	fillBasket(ctx, userID, baskets.FeedA, quotas, semanticTagCloud)
	fillBasket(ctx, userID, baskets.FeedB, quotas, semanticTagCloud)
	fillBasket(ctx, userID, baskets.FeedC, quotas, semanticTagCloud)

	return baskets, nil
}

// collectSingleBasket construit un unique panier avec son ADN strict (Cas d'Extension).
// Utilise la Seed du flux actif pour garantir la continuité de l'identité algorithmique.
func collectSingleBasket(ctx context.Context, userID int64, activeSeed int64, quotas Quotas) (*candidateBasket, error) {
	if err := quotas.validate(); err != nil {
		return nil, err
	}

	// Astuce d'orchestration : On utilise la mécanique FeedBaskets pour charger
	// la boîte aux lettres, mais on ne garde et ne remplit que le panier A.
	baskets := newFeedBaskets(quotas.MaxCandidates, activeSeed, activeSeed, activeSeed)
	err := baskets.loadSocialMailbox(ctx, userID)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("collectSingleBasket: Échec du chargement de la boîte aux lettres")
		return nil, err
	}

	singleBasket := baskets.FeedA
	semanticTagCloud := buildTagCloud(ctx, userID)

	fillBasket(ctx, userID, singleBasket, quotas, semanticTagCloud)

	return singleBasket, nil
}

// ############################################################################
// # CRÉATION DU NUAGE SÉMANTIQUE (L'Effet Pingouin)
// ############################################################################

// buildTagCloud abstrait la création du Super-Nuage sémantique pour éviter la duplication.
// Interroge la télémétrie, le graphe 1-Hop et les top tendances mondiales.
func buildTagCloud(ctx context.Context, userID int64) map[string]float64 {

	userTelemetryMap := make(map[string]float64)

	// 1. Lecture de la Télémétrie Personnelle (L1 Speed Cache)
	profile, err := cache_service.GetTelemetryProfile(ctx, userID)
	topPersonalTags := profile.TopTags

	if err == nil && len(topPersonalTags) > 0 {
		var currentWeight = 1.0
		for _, tag := range topPersonalTags {
			userTelemetryMap[tag] = currentWeight
			currentWeight -= 0.1
			if currentWeight < 0.1 {
				currentWeight = 0.1
			}
		}
	} else {
		// FALLBACK : Si le profil est vierge (Nouvel Utilisateur), on le branche sur le Top Mondial
		leaderboardData, errLeaderboard := redis.ZRevRangeWithScores(ctx, variables.RedisKeyHashtagLeaderboard, 0, 4)
		if errLeaderboard == nil && len(leaderboardData) > 0 {
			var currentWeight = 1.0
			for _, zData := range leaderboardData {
				tagName := zData.Member.(string)
				userTelemetryMap[tagName] = currentWeight
				currentWeight -= 0.1
			}
		} else {
			userTelemetryMap["bienvenue"] = 1.0 // Sécurité absolue
		}
	}

	// 1.5 OPTIMISATION "JUSTIN BIEBER" : Injection des auteurs suivis dans le nuage
	// On récupère les relations suivies en O(log N) RAM.
	if followedIDs, errRel := cache_service.GetSpeedRelationsIndex(ctx, userID); errRel == nil && len(followedIDs) > 0 {
		for _, followedID := range followedIDs {
			vipTag := fmt.Sprintf("user_%d", followedID)
			userTelemetryMap[vipTag] = 1.0 // Poids maximum pour les créateurs choisis
		}
	}

	// 2. Lecture du Leaderboard pour le calcul des Multiplicateurs de Viralité
	leaderboardData, err := redis.ZRevRangeWithScores(ctx, variables.RedisKeyHashtagLeaderboard, 0, 49)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("buildTagCloud: Échec de la lecture du leaderboard")
		return nil
	}
	leaderboardBoostsMap := make(map[string]float64)

	if len(leaderboardData) > 0 {
		maxGlobalScore := leaderboardData[0].Score
		for _, zData := range leaderboardData {
			if maxGlobalScore > 0 {
				tagName := zData.Member.(string)
				// Le boost de viralité va de 1.0 à 1.5 selon le score du tag vs le Tag #1 Mondial
				leaderboardBoostsMap[tagName] = 1.0 + (zData.Score / maxGlobalScore * 0.5)
			}
		}
	}

	// 3. Expansion Sémantique (Graphe 1-Hop) et Assemblage du Super-Nuage
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

		// Recherche des "cousins sémantiques" (1-Hop) via le Graph Cache
		neighborTags := cache_service.GetRelatedTagsLazy(ctx, coreTag)
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
// # LE REMPLISSAGE (Gestion du Quota et Dérive Sémantique)
// ############################################################################

// fillBasket remplit un panier spécifique en respectant les quotas et en appliquant l'expansion dynamique.
func fillBasket(ctx context.Context, userID int64, basket *candidateBasket, quotas Quotas, initialTagCloud map[string]float64) {
	globalTargetQuota := int(float64(quotas.MaxCandidates) * quotas.GlobalRatio)
	tagTargetQuota := int(float64(quotas.MaxCandidates) * quotas.TagRatio)

	// ─────────────────────────────────────────────────────────────────────────────
	// PHASE 1 : COLLECTE GLOBALE (SÉRENDIPITÉ PURE)
	// ─────────────────────────────────────────────────────────────────────────────
	currentDateStr := time.Now().UTC().Format("20060102")
	globalTrendKey := fmt.Sprintf(variables.RedisKeyTrendGlobalDaily, currentDateStr)

	successfullyAddedGlobally := basket.fetchDeterministicallyFromZSET(ctx, userID, globalTrendKey, globalTargetQuota, originGlobal)

	// Gestion du déficit : si on a épuisé le ZSET global (très rare), on reporte la charge sur les tags
	if successfullyAddedGlobally < globalTargetQuota {
		deficit := globalTargetQuota - successfullyAddedGlobally
		tagTargetQuota += deficit
	}

	// ─────────────────────────────────────────────────────────────────────────────
	// PHASE 2 : COLLECTE CIBLÉE (NUAGE DE TAGS)
	// ─────────────────────────────────────────────────────────────────────────────
	// Clonage du nuage pour préserver la base commune aux autres paniers
	workingTagCloud := make(map[string]float64)
	for tag, weight := range initialTagCloud {
		workingTagCloud[tag] = weight
	}

	successfullyAddedFromTags := 0
	currentGraphDepth := 1
	const maxGraphDepth = 2 // 1 = Nuage initial, 2 = Cousins directs (1-Hop). On bloque ensuite.

	// Boucle dynamique : on itère tant qu'il manque des posts et qu'on n'a pas atteint le fond du graphe autorisé
	for successfullyAddedFromTags < tagTargetQuota && currentGraphDepth <= maxGraphDepth {

		var totalCloudWeight = 0.0
		for _, weight := range workingTagCloud {
			totalCloudWeight += weight
		}

		// Tirage proportionnel dans le nuage actuel
		for tag, weight := range workingTagCloud {
			if successfullyAddedFromTags >= tagTargetQuota {
				break
			}

			// Demande proportionnelle au poids du tag dans le nuage face au quota restant
			targetForThisTag := int(math.Ceil(float64(tagTargetQuota-successfullyAddedFromTags) * (weight / totalCloudWeight)))
			if targetForThisTag <= 0 {
				targetForThisTag = 1
			}

			tagRedisKey := fmt.Sprintf(variables.RedisKeyTrendTagDaily, tag, currentDateStr)
			addedCount := basket.fetchDeterministicallyFromZSET(ctx, userID, tagRedisKey, targetForThisTag, originTag)

			successfullyAddedFromTags += addedCount
		}

		if successfullyAddedFromTags >= tagTargetQuota {
			break // Objectif final atteint
		}

		// EXPANSION DYNAMIQUE (Si le quota n'est pas rempli, on creuse d'un niveau dans le graphe)
		newDiscoveredTags := make(map[string]float64)
		for tag, weight := range workingTagCloud {
			neighborTags := cache_service.GetRelatedTagsLazy(ctx, tag)
			for neighborTag, edgeWeight := range neighborTags {
				// Si c'est un nouveau tag, on l'ajoute avec un poids atténué (-20%)
				if _, alreadyExists := workingTagCloud[neighborTag]; !alreadyExists {
					newDiscoveredTags[neighborTag] = weight * edgeWeight * 0.8
				}
			}
		}

		// UX LIMIT : On bloque la dérive sémantique stricte. Si plus rien à découvrir, on stoppe.
		if len(newDiscoveredTags) == 0 || currentGraphDepth >= maxGraphDepth {
			break
		}

		// Fusion des nouveaux tags découverts pour la prochaine itération
		for tag, weight := range newDiscoveredTags {
			workingTagCloud[tag] = weight
		}
		currentGraphDepth++
	}

	// ─────────────────────────────────────────────────────────────────────────────
	// PHASE 3 : FALLBACK UX (LE DÉFICIT DE L'ÉLÉPHANT)
	// ─────────────────────────────────────────────────────────────────────────────
	// Si l'utilisateur a une niche très étroite qui est épuisée, on comble le déficit
	// avec du contenu Viral Mondial plutôt que de proposer du hors-sujet.
	if successfullyAddedFromTags < tagTargetQuota {
		deficit := tagTargetQuota - successfullyAddedFromTags
		basket.fetchDeterministicallyFromZSET(ctx, userID, globalTrendKey, deficit, originGlobal)
	}
}
