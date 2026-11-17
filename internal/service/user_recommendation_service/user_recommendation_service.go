package user_recommendation_service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	pgRepo "github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	goredis "github.com/go-redis/redis/v8"
	"github.com/vmihailenco/msgpack/v5"
)

// ############################################################################
// # SERVICE : RECOMMANDATION D'UTILISATEURS (STRICTEMENT ISOLÉ DU CONTENU)
// ############################################################################

// EnqueueGenerateUserRecommendations soumet un job asynchrone ActionGenerateUserRecommendations dans la file Redis.
func EnqueueGenerateUserRecommendations(ctx context.Context, userID int64, force bool, triggeredBy string) error {
	if userID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Identifiant utilisateur invalide.", nil)
	}
	payload := user_recommendation_models.GenerateUserRecommendationsJobPayload{
		UserID:      userID,
		Force:       force,
		TriggeredBy: triggeredBy,
		RequestedAt: domain.NowMillis(),
	}
	if err := payload.Validate(); err != nil {
		return err
	}
	return redis.EnqueueDB(
		ctx,
		userID,
		userID,
		redis.EntityUserRecommendation,
		redis.ActionGenerateUserRecommendations,
		payload,
		redis.TargetWorker,
	)
}

// GetUserRecommendations récupère la liste paginée d'utilisateurs recommandés depuis le cache L1 Redis (sub-5ms),
// gère le recalcul forcé (avec verrou de 12h) et le cache miss (Lazy Evaluation), puis hydrate les UserLiteView.
func GetUserRecommendations(
	ctx context.Context,
	callerID int64,
	input user_recommendation_models.GetUserRecommendationsInput,
) (user_recommendation_models.GetUserRecommendationsOutput, error) {
	if callerID <= 0 {
		return user_recommendation_models.GetUserRecommendationsOutput{}, numan_error.NewUnauthorized(numan_error.CodeUnauthorized, "Authentification requise.", nil)
	}
	if err := input.Validate(); err != nil {
		return user_recommendation_models.GetUserRecommendationsOutput{}, err
	}

	jobEnqueued := false

	// ── ÉTAPE 1 : GESTION DU RECALCUL FORCÉ (FORCE=TRUE + VERROU 12H) ───────
	if input.Force {
		acquired, errLock := cache_service.AcquireUserRecoComputeLock(ctx, callerID)
		if errLock != nil {
			numan_log.Warn(ctx).Err(errLock).Int64("user_id", callerID).Msg("Avertissement lors de la vérification du verrou de calcul de recommandation")
		} else if acquired {
			if errEnq := EnqueueGenerateUserRecommendations(ctx, callerID, true, "http_force"); errEnq != nil {
				numan_log.Warn(ctx).Err(errEnq).Int64("user_id", callerID).Msg("Échec de la mise en file d'attente du recalcul forcé de recommandations")
			} else {
				jobEnqueued = true
			}
		}
	}

	// ── ÉTAPE 2 : LECTURE ULTRA-RAPIDE EN CACHE L1 (ZSET) ───────────────────
	recommendedIDs, cacheHit, errCache := cache_service.GetUserRecommendationIDs(ctx, callerID, input.Sort, input.Limit)
	if errCache != nil {
		numan_log.Error(ctx).Err(errCache).Int64("user_id", callerID).Str("sort", input.Sort).Msg("Erreur L1 lors de la lecture des recommandations d'utilisateurs")
		return user_recommendation_models.GetUserRecommendationsOutput{}, errCache
	}

	// ── ÉTAPE 3 : CACHE MISS (LAZY EVALUATION ASYNCHRONE + ATTENTE BORNÉE) ──
	if !cacheHit {
		if !jobEnqueued {
			if errEnq := EnqueueGenerateUserRecommendations(ctx, callerID, false, "cache_miss"); errEnq != nil {
				numan_log.Warn(ctx).Err(errEnq).Int64("user_id", callerID).Msg("Échec de la mise en file d'attente du calcul initial de recommandations")
			}
		}

		waitTimeout := variables.UserRecoSyncWaitTimeout
		if waitTimeout > 0 {
			deadline := time.Now().Add(waitTimeout)
			for time.Now().Before(deadline) {
				select {
				case <-ctx.Done():
					return user_recommendation_models.GetUserRecommendationsOutput{
						Users: make([]auth_models.UserLiteView, 0),
					}, nil
				case <-time.After(15 * time.Millisecond):
				}
				ids, hit, errPoll := cache_service.GetUserRecommendationIDs(ctx, callerID, input.Sort, input.Limit)
				if errPoll == nil && hit {
					recommendedIDs = ids
					break
				}
			}
		}
	}

	if len(recommendedIDs) == 0 {
		return user_recommendation_models.GetUserRecommendationsOutput{
			Users: make([]auth_models.UserLiteView, 0),
		}, nil
	}

	// ── ÉTAPE 4 : HYDRATATION USERLITEVIEW EN PRÉSERVANT L'ORDRE EXACT ──────
	hydratedUsers := hydrateRecommendedUsers(ctx, callerID, recommendedIDs)
	return user_recommendation_models.GetUserRecommendationsOutput{
		Users: hydratedUsers,
	}, nil
}

// hydrateRecommendedUsers hydrate une tranche d'identifiants recommandés en UserLiteView
// via un MGET groupé en L1 suivi d'un fallback unitaire, tout en préservant l'ordre exact de classement Redis.
func hydrateRecommendedUsers(ctx context.Context, callerID int64, recommendedIDs []int64) []auth_models.UserLiteView {
	if len(recommendedIDs) == 0 {
		return make([]auth_models.UserLiteView, 0)
	}

	liteByID := make(map[int64]lite_models.UserLiteRequest, len(recommendedIDs))

	if redis.UsersLite != nil {
		if mgetRes, errMGet := redis.UsersLite.GetMany(ctx, recommendedIDs); errMGet == nil && mgetRes != nil {
			for uID, rawBytes := range mgetRes.Found {
				var uLite lite_models.UserLiteRequest
				if errUnmarshal := msgpack.Unmarshal(rawBytes, &uLite); errUnmarshal == nil && uLite.ID != 0 {
					liteByID[uID] = uLite
				}
			}
		}
	}

	hydrated := make([]auth_models.UserLiteView, 0, len(recommendedIDs))
	for _, targetID := range recommendedIDs {
		if targetID <= 0 || targetID == callerID {
			continue
		}
		uLite, found := liteByID[targetID]
		if !found {
			fetchedLite, errFetch := cache_service.GetUserLite(ctx, targetID)
			if errFetch != nil || fetchedLite.ID == 0 {
				continue
			}
			uLite = fetchedLite
		}

		var avatarView media_models.MediaView
		if uLite.ProfilePictureID > 0 {
			if generatedAvatar, errMedia := media_service.GenerateMediaViewCascade(ctx, uLite.ProfilePictureID, uLite.ID, 0, callerID); errMedia == nil {
				avatarView = generatedAvatar
			}
		}

		isOnline := false
		if uLite.ShowOnlineStatus {
			isOnline = cache_service.IsUserOnline(ctx, uLite.ID)
		}

		hydrated = append(hydrated, auth_models.UserLiteView{
			User:     uLite,
			Avatar:   avatarView,
			IsOnline: isOnline,
		})
	}

	return hydrated
}

// SkipUserRecommendation enregistre l'exclusion d'un utilisateur recommandé :
// 1. Validation & interdiction du self-skip
// 2. Insertion synchrone dans le Cuckoo Filter Redis L1 (+ retrait immédiat des ZSETs L1)
// 3. Persistance asynchrone vers MongoDB L2 (user_reco_skips) via redis.EnqueueDB
func SkipUserRecommendation(
	ctx context.Context,
	callerID int64,
	input user_recommendation_models.SkipUserRecommendationInput,
) (user_recommendation_models.SkipUserRecommendationOutput, error) {
	if callerID <= 0 {
		return user_recommendation_models.SkipUserRecommendationOutput{}, numan_error.NewUnauthorized(numan_error.CodeUnauthorized, "Authentification requise.", nil)
	}
	if err := input.Validate(); err != nil {
		return user_recommendation_models.SkipUserRecommendationOutput{}, err
	}
	if callerID == input.SkippedUserID {
		return user_recommendation_models.SkipUserRecommendationOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Un utilisateur ne peut pas s'ignorer lui-même.",
			nil,
		)
	}

	// 1. Écriture synchrone dans le Cuckoo Filter L1 Redis (succès obligatoire pour valider la requête)
	if errL1 := cache_service.AddUserRecoSkipToCuckoo(ctx, callerID, input.SkippedUserID); errL1 != nil {
		numan_log.Error(ctx).Err(errL1).Int64("user_id", callerID).Int64("skipped_user_id", input.SkippedUserID).Msg("Échec L1 lors de l'ajout du skip dans le Cuckoo Filter")
		return user_recommendation_models.SkipUserRecommendationOutput{}, errL1
	}

	// 2. Persistance asynchrone vers MongoDB L2 (collection user_reco_skips)
	skipPayload := user_recommendation_models.UserRecoSkipPayload{
		UserID:        callerID,
		SkippedUserID: input.SkippedUserID,
		CreatedAt:     time.Now().UTC(),
	}
	if errQueue := redis.EnqueueDB(
		ctx,
		callerID,
		input.SkippedUserID,
		redis.EntityUserRecoSkip,
		redis.ActionCreate,
		skipPayload,
		redis.TargetMongo,
	); errQueue != nil {
		numan_log.Warn(ctx).Err(errQueue).Int64("user_id", callerID).Int64("skipped_user_id", input.SkippedUserID).Action(numan_log.ActionSync).Msg("Échec de la mise en file d'attente L2 du skip de recommandation")
	}

	return user_recommendation_models.SkipUserRecommendationOutput{
		Status:        "ok",
		SkippedUserID: input.SkippedUserID,
	}, nil
}

// ============================================================================
// MOTEUR ASYNCHRONE : EXÉCUTION DES 3 PHASES DE RECOMMANDATION
// ============================================================================

// GenerateUserRecommendations exécute le pipeline complet en 3 phases pour un utilisateur cible :
// Phase 1 : Pré-filtrage des candidats (Langue SINTER, Cuckoo Filter Skips, Relations sociales, Sécurité)
// Phase 2 : Scoring Mathématique (Multiplicateur Communautaire TF-IDF, Taste SIMD [224]float32, Affinité Sociale, Géolocalisation)
// Phase 3 : Classement Top-150 par catégorie, Interleaving Round-Robin Global et Publication Atomique Redis.
func GenerateUserRecommendations(ctx context.Context, targetUserID int64) error {
	if targetUserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Identifiant utilisateur cible invalide.", nil)
	}

	startTime := time.Now()

	// ── CHARGEMENT DU PROFIL ET DES PARAMÈTRES DE L'UTILISATEUR CIBLE ───────
	targetUser, targetSettings, isTargetEligible := loadUserProfileAndSettings(ctx, targetUserID)
	if !isTargetEligible {
		// L'utilisateur cible est banni, désactivé ou introuvable : publication de listes vides
		return cache_service.PublishUserRecommendationLists(ctx, targetUserID, nil, nil, nil, nil)
	}

	targetLang := targetSettings.DisplayAndContent.Language
	if targetLang < 0 {
		targetLang = 0
	}

	// Récupération en lecture seule du vecteur d'intérêt client [224]float32 (Edge Computing)
	var targetVector *[VectorDimension]float32
	if telemetryProfile, errTel := cache_service.GetTelemetryProfile(ctx, targetUserID); errTel == nil {
		if vecArr, okVec := IsNonZeroVector224(telemetryProfile.Vector); okVec {
			targetVector = vecArr
		}
	} else if vecArr, okVec := IsNonZeroVector224(targetSettings.TelemetryVector); okVec {
		targetVector = vecArr
	}

	// ========================================================================
	// PHASE 1 — PRÉ-FILTRAGE DES CANDIDATS (~1,000–5,000 CANDIDATS VIABLES)
	// ========================================================================

	// 1. Filtrage linguistique via Redis SINTER sur users:lang:{lang}
	langCandidates, errLang := cache_service.GetCandidatesByLanguages(ctx, targetLang)
	if errLang != nil {
		return errLang
	}
	if len(langCandidates) == 0 {
		return cache_service.PublishUserRecommendationLists(ctx, targetUserID, nil, nil, nil, nil)
	}

	// 2. Exclusion par le Cuckoo Filter (avec reconstruction automatique depuis MongoDB si absent)
	nonSkippedCandidates, errSkip := cache_service.FilterOutSkippedCandidates(ctx, targetUserID, langCandidates)
	if errSkip != nil {
		return errSkip
	}
	if len(nonSkippedCandidates) == 0 {
		return cache_service.PublishUserRecommendationLists(ctx, targetUserID, nil, nil, nil, nil)
	}

	// 3. Exclusion sociale (amis existants, abonnements, utilisateurs bloqués)
	excludedSocialMap, targetFriendsSet := loadSocialExclusionsAndTargetFriends(ctx, targetUserID)

	// 4. Filtres de sécurité (auto-exclusion, comptes supprimés/désactivés, bannis, inéligibles)
	maxCandidates := variables.UserRecoMaxCandidates
	if maxCandidates <= 0 {
		maxCandidates = 5000
	}

	preFilteredIDs := make([]int64, 0, len(nonSkippedCandidates))
	seenCandidate := make(map[int64]struct{}, len(nonSkippedCandidates))
	for _, candID := range nonSkippedCandidates {
		if candID <= 0 || candID == targetUserID {
			continue
		}
		if _, alreadySeen := seenCandidate[candID]; alreadySeen {
			continue
		}
		if _, isExcluded := excludedSocialMap[candID]; isExcluded {
			continue
		}
		seenCandidate[candID] = struct{}{}
		preFilteredIDs = append(preFilteredIDs, candID)
		if len(preFilteredIDs) >= maxCandidates {
			break
		}
	}

	if len(preFilteredIDs) == 0 {
		return cache_service.PublishUserRecommendationLists(ctx, targetUserID, nil, nil, nil, nil)
	}

	// Vérification groupée d'éligibilité des candidats (non bannis, non désactivés)
	viableCandidates, candidateMetaMap := filterEligibleCandidatesBatch(ctx, preFilteredIDs)
	if len(viableCandidates) == 0 {
		return cache_service.PublishUserRecommendationLists(ctx, targetUserID, nil, nil, nil, nil)
	}

	// ========================================================================
	// PHASE 2 — MOTEUR DE SCORING MATHÉMATIQUE (BATCH, SANS REQUÊTES N+1)
	// ========================================================================

	// 2.1. Chargement groupé des communautés partagées et calcul du multiplicateur TF-IDF
	totalActiveUsers := estimateTotalActiveUsers(ctx, targetLang, len(langCandidates))
	sharedCommunitySizesByCand := loadSharedCommunitySizesBatch(ctx, targetUserID, viableCandidates)

	// 2.2. Chargement groupé des vecteurs d'intérêt [224]float32 des candidats (lecture seule)
	candidateVectorsMap := loadCandidateVectorsBatch(ctx, viableCandidates)

	// 2.3. Chargement groupé des amis en commun par Pipeline Redis (0 requête N+1)
	mutualFriendsCountMap := countMutualFriendsBatch(ctx, targetFriendsSet, viableCandidates)

	// 2.4. Calcul des 3 scores (Geo, Affinity, Taste) pour chaque candidat viable
	omega := variables.UserRecoMutualFriendWeight
	geoScaleKm := variables.UserRecoGeoDecayScaleKm
	targetLocation := targetUser.Location
	targetShowLoc := targetSettings.Privacy.ShowLocation

	scoredCandidates := make([]ScoredCandidate, 0, len(viableCandidates))

	for _, candID := range viableCandidates {
		commMultiplier := ComputeCommunityMultiplier(totalActiveUsers, sharedCommunitySizesByCand[candID])

		// Score d'Affinité Sociale
		mutualCount := mutualFriendsCountMap[candID]
		affScore := ComputeAffinityScore(mutualCount, omega, commMultiplier)

		// Score de Goût (Taste - Produit scalaire SIMD [224]float32)
		var tasteScore float64
		hasTaste := false
		if targetVector != nil {
			if candVec, okCandVec := candidateVectorsMap[candID]; okCandVec && candVec != nil {
				tasteScore = ComputeTasteScore(targetVector, candVec, commMultiplier)
				hasTaste = true
			}
		}

		// Score Géographique (Haversine + Décroissance Exponentielle)
		candMeta := candidateMetaMap[candID]
		proxFactor, hasGeo := ComputeGeoProximityFactor(
			targetLocation,
			targetShowLoc,
			candMeta.Location,
			candMeta.ShowLocation,
			geoScaleKm,
		)
		var geoScore float64
		if hasGeo {
			geoScore = ComputeGeoScore(proxFactor, commMultiplier)
		}

		scoredCandidates = append(scoredCandidates, ScoredCandidate{
			UserID:              candID,
			GeoScore:            geoScore,
			HasGeo:              hasGeo,
			AffScore:            affScore,
			TasteScore:          tasteScore,
			HasTaste:            hasTaste,
			CommunityMultiplier: commMultiplier,
		})
	}

	// ========================================================================
	// PHASE 3 — AGRÉGATION, INTERLEAVING ROUND-ROBIN & PUBLICATION ATOMIQUE
	// ========================================================================

	topK := variables.UserRecoTopK
	if topK <= 0 {
		topK = 150
	}

	geoTop, affTop, tasteTop := BuildCategoryRankings(scoredCandidates, topK)
	globalTop := InterleaveGlobalRecommendations(geoTop, affTop, tasteTop, topK)

	if errPub := cache_service.PublishUserRecommendationLists(ctx, targetUserID, geoTop, affTop, tasteTop, globalTop); errPub != nil {
		return errPub
	}

	numan_log.Info(ctx).
		Int64("user_id", targetUserID).
		Int("candidates_viable", len(viableCandidates)).
		Int("geo_count", len(geoTop)).
		Int("aff_count", len(affTop)).
		Int("taste_count", len(tasteTop)).
		Int("global_count", len(globalTop)).
		Int64("duration_ms", time.Since(startTime).Milliseconds()).
		Msg("Recommandations d'utilisateurs calculées et publiées avec succès en L1")

	return nil
}

// ============================================================================
// FONCTIONS D'ASSISTANCE BATCH POUR LES PHASES 1 ET 2
// ============================================================================

type candidateMetadata struct {
	Location     string
	ShowLocation bool
}

// loadUserProfileAndSettings récupère le profil et les paramètres d'un utilisateur et vérifie son éligibilité.
func loadUserProfileAndSettings(ctx context.Context, userID int64) (auth_models.UserPayload, user_settings_models.UserSettingsPayload, bool) {
	var userPayload auth_models.UserPayload
	var settingsPayload user_settings_models.UserSettingsPayload
	foundUser := false

	// 1. Tentative Object Cache L1 (Users)
	if redis.Users != nil {
		if err := redis.Users.GetObject(ctx, userID, &userPayload); err == nil && userPayload.ID != 0 {
			foundUser = true
		}
	}

	// 2. Tentative MongoDB L2
	if !foundUser && mongo.Users != nil && mongo.Users.DB != nil {
		if uMongo, errMongo := mongo.MongoLoadUser(ctx, userID, "", "", ""); errMongo == nil && uMongo.ID != 0 {
			userPayload = uMongo
			foundUser = true
		}
	}

	// 3. Tentative PostgreSQL L3
	if !foundUser && postgres.PostgresDB != nil {
		if uPg, errPg := pgRepo.FuncLoadUser(ctx, userID, "", "", ""); errPg == nil && uPg.ID != 0 {
			userPayload = uPg
			foundUser = true
		}
	}

	// 4. Fallback sur UsersLite (Speed Cache L1) si seules les données allégées sont en RAM
	if !foundUser && redis.UsersLite != nil {
		var uLite lite_models.UserLiteRequest
		if errLite := redis.UsersLite.GetObject(ctx, userID, &uLite); errLite == nil && uLite.ID != 0 {
			userPayload = auth_models.UserPayload{
				ID:           uLite.ID,
				Username:     uLite.Username,
				FirstName:    uLite.FirstName,
				LastName:     uLite.LastName,
				TrustScore:   uLite.TrustScore,
				Banned:       false,
				Desactivated: false,
			}
			foundUser = true
		}
	}

	if !foundUser || userPayload.Banned || userPayload.Desactivated {
		return auth_models.UserPayload{}, user_settings_models.UserSettingsPayload{}, false
	}

	// Paramètres utilisateur (L1 -> L2 -> L3)
	if sPayload, errSettings := object_cache_service.GetUserSettingsCascade(ctx, userID); errSettings == nil && sPayload.ID != 0 {
		settingsPayload = sPayload
	} else {
		settingsPayload.UserID = userID
		settingsPayload.Privacy.ShowLocation = targetHasDefaultShowLocation(userPayload.Location)
	}

	return userPayload, settingsPayload, true
}

func targetHasDefaultShowLocation(loc string) bool {
	return loc != ""
}

// loadSocialExclusionsAndTargetFriends charge toutes les relations directes à exclure (amis, abonnements, bloqués)
// ainsi que l'ensemble des amis de l'utilisateur cible pour le calcul des amis en commun.
func loadSocialExclusionsAndTargetFriends(ctx context.Context, userID int64) (map[int64]struct{}, map[int64]struct{}) {
	excluded := make(map[int64]struct{})
	friends := make(map[int64]struct{})

	// 1. Lecture depuis les index ZSET Redis L1 (SpeedRelationsIndex)
	if redis.SpeedRelationsIndex != nil {
		// Amis (State = 2)
		if friendStrs, err := redis.SpeedRelationsIndex.ZRevRange(ctx, fmt.Sprintf("in:%d:%d", variables.RelationStateFriend, userID), 0, -1); err == nil {
			for _, id := range pkg.ParseInt64List(friendStrs) {
				excluded[id] = struct{}{}
				friends[id] = struct{}{}
			}
		}
		if friendOutStrs, err := redis.SpeedRelationsIndex.ZRevRange(ctx, fmt.Sprintf("out:%d:%d", variables.RelationStateFriend, userID), 0, -1); err == nil {
			for _, id := range pkg.ParseInt64List(friendOutStrs) {
				excluded[id] = struct{}{}
				friends[id] = struct{}{}
			}
		}
		// Abonnements sortants (Followings : out:1:{userID})
		if followOutStrs, err := redis.SpeedRelationsIndex.ZRevRange(ctx, fmt.Sprintf("out:%d:%d", variables.RelationStateFollow, userID), 0, -1); err == nil {
			for _, id := range pkg.ParseInt64List(followOutStrs) {
				excluded[id] = struct{}{}
			}
		}
		// Utilisateurs bloqués (out:-1:{userID} et in:-1:{userID})
		if blockedOutStrs, err := redis.SpeedRelationsIndex.ZRevRange(ctx, fmt.Sprintf("out:%d:%d", variables.RelationStateBlocked, userID), 0, -1); err == nil {
			for _, id := range pkg.ParseInt64List(blockedOutStrs) {
				excluded[id] = struct{}{}
			}
		}
		if blockedInStrs, err := redis.SpeedRelationsIndex.ZRevRange(ctx, fmt.Sprintf("in:%d:%d", variables.RelationStateBlocked, userID), 0, -1); err == nil {
			for _, id := range pkg.ParseInt64List(blockedInStrs) {
				excluded[id] = struct{}{}
			}
		}
	}

	// 2. Lecture depuis le Hash SpeedRelations de l'utilisateur cible
	if redis.SpeedRelations != nil {
		if relMap, err := redis.SpeedRelations.HGetAll(ctx, userID).Result(); err == nil {
			for otherIDStr, stateStr := range relMap {
				otherID := pkg.ParseInt64(otherIDStr)
				stateVal, errConv := strconv.Atoi(stateStr)
				if otherID <= 0 || errConv != nil {
					continue
				}
				if stateVal == variables.RelationStateBlocked {
					excluded[otherID] = struct{}{}
				} else if stateVal == variables.RelationStateFriend {
					excluded[otherID] = struct{}{}
					friends[otherID] = struct{}{}
				}
			}
		}
	}

	// 3. Complément L3 PostgreSQL (auth.relations via FuncLoadAddableRelations) si initialisé
	if postgres.PostgresDB != nil {
		if addableRels, errPg := pgRepo.FuncLoadAddableRelations(ctx, userID); errPg == nil {
			for _, r := range addableRels {
				if r.TargetID <= 0 {
					continue
				}
				excluded[r.TargetID] = struct{}{}
				if r.State == variables.RelationStateFriend {
					friends[r.TargetID] = struct{}{}
				}
			}
		}
	}

	return excluded, friends
}

// filterEligibleCandidatesBatch vérifie en lot que les candidats existent et ne sont ni bannis, ni désactivés/supprimés,
// et collecte leurs métadonnées de géolocalisation sans requête N+1.
func filterEligibleCandidatesBatch(ctx context.Context, candidateIDs []int64) ([]int64, map[int64]candidateMetadata) {
	viable := make([]int64, 0, len(candidateIDs))
	metaMap := make(map[int64]candidateMetadata, len(candidateIDs))

	// 1. Batch MGET sur redis.Users (Object Cache L1)
	foundInUsers := make(map[int64]auth_models.UserPayload, len(candidateIDs))
	if redis.Users != nil {
		if res, err := redis.Users.GetMany(ctx, candidateIDs); err == nil && res != nil {
			for uID, raw := range res.Found {
				var u auth_models.UserPayload
				if err := msgpack.Unmarshal(raw, &u); err == nil && u.ID != 0 {
					foundInUsers[uID] = u
				}
			}
		}
	}

	// 2. Batch MGET sur redis.UsersLite (Speed Cache L1)
	foundInLite := make(map[int64]lite_models.UserLiteRequest, len(candidateIDs))
	if redis.UsersLite != nil {
		if res, err := redis.UsersLite.GetMany(ctx, candidateIDs); err == nil && res != nil {
			for uID, raw := range res.Found {
				var uLite lite_models.UserLiteRequest
				if err := msgpack.Unmarshal(raw, &uLite); err == nil && uLite.ID != 0 {
					foundInLite[uID] = uLite
				}
			}
		}
	}

	// 3. Batch MGET sur redis.UserSettings (Object Cache L1) pour ShowLocation
	settingsMap := make(map[int64]user_settings_models.UserSettingsPayload, len(candidateIDs))
	if redis.UserSettings != nil {
		if res, err := redis.UserSettings.GetMany(ctx, candidateIDs); err == nil && res != nil {
			for uID, raw := range res.Found {
				var s user_settings_models.UserSettingsPayload
				if err := msgpack.Unmarshal(raw, &s); err == nil && s.UserID != 0 {
					settingsMap[uID] = s
				}
			}
		}
	}

	for _, candID := range candidateIDs {
		if uFull, okFull := foundInUsers[candID]; okFull {
			if uFull.Banned || uFull.Desactivated {
				continue
			}
			showLoc := uFull.Location != ""
			if s, okS := settingsMap[candID]; okS {
				showLoc = s.Privacy.ShowLocation
			}
			viable = append(viable, candID)
			metaMap[candID] = candidateMetadata{
				Location:     uFull.Location,
				ShowLocation: showLoc,
			}
			continue
		}

		if uLite, okLite := foundInLite[candID]; okLite && uLite.ID != 0 {
			showLoc := false
			if s, okS := settingsMap[candID]; okS {
				showLoc = s.Privacy.ShowLocation
			}
			viable = append(viable, candID)
			metaMap[candID] = candidateMetadata{
				Location:     "",
				ShowLocation: showLoc,
			}
		}
	}

	return viable, metaMap
}

// estimateTotalActiveUsers estime le nombre total d'utilisateurs N pour le calcul TF-IDF.
func estimateTotalActiveUsers(ctx context.Context, targetLang int, fallbackCount int) int64 {
	var total int64
	if redis.UsersLex != nil {
		if card, err := redis.UsersLex.ZCard(ctx, variables.LexicographicGlobalKey); err == nil && card > total {
			total = card
		}
	}
	if redis.UsersLang != nil {
		if card, err := redis.UsersLang.SCard(ctx, targetLang); err == nil && card > total {
			total = card
		}
	}
	if int64(fallbackCount) > total {
		total = int64(fallbackCount)
	}
	if total < 10 {
		total = 10
	}
	return total
}

// loadSharedCommunitySizesBatch identifie les communautés de l'utilisateur cible et récupère en lot (Pipeline Redis)
// leur taille |c| ainsi que les candidats qui en sont également membres.
func loadSharedCommunitySizesBatch(ctx context.Context, targetUserID int64, viableCandidates []int64) map[int64][]int64 {
	result := make(map[int64][]int64)
	if len(viableCandidates) == 0 || redis.UserInbox == nil || redis.ConvParticipants == nil {
		return result
	}

	// Récupération des conversations/communautés de l'utilisateur cible depuis son Inbox L1
	convIDStrs, err := redis.UserInbox.ZRevRange(ctx, targetUserID, 0, -1)
	if err != nil || len(convIDStrs) == 0 {
		return result
	}
	convIDs := pkg.ParseInt64List(convIDStrs)
	if len(convIDs) == 0 {
		return result
	}

	// Vérification de la taille |c| de chaque communauté via SpeedCommunity (MGET) ou SCard sur ConvParticipants
	communitySizes := make(map[int64]int64, len(convIDs))
	if redis.SpeedCommunity != nil {
		if mgetComm, errMGet := redis.SpeedCommunity.GetMany(ctx, convIDs); errMGet == nil && mgetComm != nil {
			for cID, raw := range mgetComm.Found {
				var commLite lite_models.CommunityLiteRequest
				if err := msgpack.Unmarshal(raw, &commLite); err == nil && commLite.ID != 0 && commLite.MemberCount > 0 {
					communitySizes[cID] = int64(commLite.MemberCount)
				}
			}
		}
	}

	// Chargement groupé des participants de chaque communauté via un Pipeline Redis unique
	pipe := redis.ConvParticipants.Client.Pipeline()
	membersCmds := make(map[int64]*goredis.StringSliceCmd, len(convIDs))
	for _, cID := range convIDs {
		membersCmds[cID] = pipe.SMembers(ctx, redis.ConvParticipants.Key(cID))
	}
	_, _ = pipe.Exec(ctx)

	candidateSet := make(map[int64]struct{}, len(viableCandidates))
	for _, candID := range viableCandidates {
		candidateSet[candID] = struct{}{}
	}

	for _, cID := range convIDs {
		cmd := membersCmds[cID]
		if cmd == nil {
			continue
		}
		memberStrs, errCmd := cmd.Result()
		if errCmd != nil || len(memberStrs) == 0 {
			continue
		}
		memberIDs := pkg.ParseInt64List(memberStrs)
		cSize := communitySizes[cID]
		if cSize <= 0 {
			cSize = int64(len(memberIDs))
		}
		if cSize <= 0 {
			continue
		}

		for _, mID := range memberIDs {
			if _, isViableCandidate := candidateSet[mID]; isViableCandidate {
				result[mID] = append(result[mID], cSize)
			}
		}
	}

	return result
}

// loadCandidateVectorsBatch charge en lot (MGET L1) les vecteurs d'intérêt [224]float32 des candidats.
// Ne génère ni ne modifie jamais aucun vecteur côté serveur.
func loadCandidateVectorsBatch(ctx context.Context, viableCandidates []int64) map[int64]*[VectorDimension]float32 {
	vectors := make(map[int64]*[VectorDimension]float32, len(viableCandidates))
	if len(viableCandidates) == 0 {
		return vectors
	}

	// 1. MGET massif sur redis.TelemetryProfiles
	if redis.TelemetryProfiles != nil {
		if mgetRes, err := redis.TelemetryProfiles.GetMany(ctx, viableCandidates); err == nil && mgetRes != nil {
			for uID, raw := range mgetRes.Found {
				var profile lite_models.TelemetryProfileLite
				if errUnmarshal := msgpack.Unmarshal(raw, &profile); errUnmarshal == nil {
					if vecArr, ok := IsNonZeroVector224(profile.Vector); ok {
						vectors[uID] = vecArr
					}
				}
			}
		}
	}

	// 2. Fallback MGET sur redis.UserSettings pour les candidats non trouvés dans TelemetryProfiles
	var missingIDs []int64
	for _, uID := range viableCandidates {
		if _, found := vectors[uID]; !found {
			missingIDs = append(missingIDs, uID)
		}
	}
	if len(missingIDs) > 0 && redis.UserSettings != nil {
		if mgetSettings, err := redis.UserSettings.GetMany(ctx, missingIDs); err == nil && mgetSettings != nil {
			for uID, raw := range mgetSettings.Found {
				var s user_settings_models.UserSettingsPayload
				if errUnmarshal := msgpack.Unmarshal(raw, &s); errUnmarshal == nil {
					if vecArr, ok := IsNonZeroVector224(s.TelemetryVector); ok {
						vectors[uID] = vecArr
					}
				}
			}
		}
	}

	return vectors
}

// countMutualFriendsBatch calcule en un seul Pipeline Redis le nombre d'amis en commun
// entre l'utilisateur cible et chaque candidat viable (aucune requête N+1).
func countMutualFriendsBatch(
	ctx context.Context,
	targetFriends map[int64]struct{},
	viableCandidates []int64,
) map[int64]int {
	counts := make(map[int64]int, len(viableCandidates))
	if len(targetFriends) == 0 || len(viableCandidates) == 0 || redis.SpeedRelationsIndex == nil {
		return counts
	}

	pipe := redis.SpeedRelationsIndex.Client.Pipeline()
	cmds := make(map[int64]*goredis.StringSliceCmd, len(viableCandidates))
	for _, candID := range viableCandidates {
		zsetKey := redis.SpeedRelationsIndex.Key(fmt.Sprintf("in:%d:%d", variables.RelationStateFriend, candID))
		cmds[candID] = pipe.ZRevRange(ctx, zsetKey, 0, -1)
	}
	_, _ = pipe.Exec(ctx)

	for _, candID := range viableCandidates {
		cmd := cmds[candID]
		if cmd == nil {
			continue
		}
		friendStrs, err := cmd.Result()
		if err != nil || len(friendStrs) == 0 {
			continue
		}
		mutual := 0
		for _, fID := range pkg.ParseInt64List(friendStrs) {
			if _, isTargetFriend := targetFriends[fID]; isTargetFriend {
				mutual++
			}
		}
		if mutual > 0 {
			counts[candID] = mutual
		}
	}

	return counts
}
