package cache_service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	goredis "github.com/go-redis/redis/v8"
)

// ############################################################################
// # SERVICE CACHE L1 : SYSTÈME DE RECOMMANDATION D'UTILISATEURS
// ############################################################################

// UserRecoScoredEntry représente un candidat classé avec son score pour un ZSET Redis.
type UserRecoScoredEntry struct {
	UserID int64
	Score  float64
}

// resolveUserRecoCollection sélectionne la collection ZSET Redis correspondant au mode de tri.
func resolveUserRecoCollection(sortMode string) (*redis.Collection, error) {
	switch sortMode {
	case "", variables.UserRecoSortGlobal:
		return redis.UserRecoListGlobal, nil
	case variables.UserRecoSortGeo:
		return redis.UserRecoListGeo, nil
	case variables.UserRecoSortAff:
		return redis.UserRecoListAff, nil
	case variables.UserRecoSortTaste:
		return redis.UserRecoListTaste, nil
	default:
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le mode de tri de recommandation est invalide.",
			fmt.Errorf("unsupported recommendation sort mode: %s", sortMode),
		)
	}
}

// ============================================================================
// 1. INDEX DE LANGUE (users:lang:{lang})
// ============================================================================

// AddUserToLanguageIndex indexe un utilisateur dans le SET Redis associé à sa langue.
func AddUserToLanguageIndex(ctx context.Context, userID int64, lang int) error {
	if userID <= 0 || lang < 0 {
		return nil
	}
	if redis.UsersLang == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Le cache d'indexation linguistique est indisponible.",
			fmt.Errorf("redis.UsersLang collection is nil"),
		)
	}
	if err := redis.UsersLang.SAdd(ctx, lang, userID); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'indexer la langue de l'utilisateur dans le cache.",
			err,
		)
	}
	return nil
}

// SwitchUserLanguageIndex déplace atomiquement un utilisateur d'un index de langue vers un autre.
func SwitchUserLanguageIndex(ctx context.Context, userID int64, oldLang int, newLang int) error {
	if userID <= 0 {
		return nil
	}
	if redis.UsersLang == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Le cache d'indexation linguistique est indisponible.",
			fmt.Errorf("redis.UsersLang collection is nil"),
		)
	}
	if oldLang >= 0 && oldLang != newLang {
		if err := redis.UsersLang.SRem(ctx, oldLang, userID); err != nil {
			numan_log.Warn(ctx).Err(err).Int64("user_id", userID).Int("old_lang", oldLang).Msg("Échec du retrait de l'ancien index de langue Redis")
		}
	}
	if newLang >= 0 {
		if err := redis.UsersLang.SAdd(ctx, newLang, userID); err != nil {
			return numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeCacheError,
				"Impossible de mettre à jour l'index de langue de l'utilisateur dans le cache.",
				err,
			)
		}
	}
	return nil
}

// GetCandidatesByLanguages exécute SINTER sur users:lang:{lang} pour pré-filtrer les candidats.
func GetCandidatesByLanguages(ctx context.Context, langs ...int) ([]int64, error) {
	if len(langs) == 0 {
		return []int64{}, nil
	}
	if redis.UsersLang == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Le cache d'indexation linguistique est indisponible.",
			fmt.Errorf("redis.UsersLang collection is nil"),
		)
	}

	subKeys := make([]any, 0, len(langs))
	seenLang := make(map[int]struct{}, len(langs))
	for _, l := range langs {
		if l < 0 {
			continue
		}
		if _, exists := seenLang[l]; !exists {
			seenLang[l] = struct{}{}
			subKeys = append(subKeys, l)
		}
	}
	if len(subKeys) == 0 {
		return []int64{}, nil
	}

	members, err := redis.UsersLang.SInter(ctx, subKeys...)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'exécuter l'intersection linguistique dans Redis.",
			err,
		)
	}

	parsed := pkg.ParseInt64List(members)
	if parsed == nil {
		return []int64{}, nil
	}
	return parsed, nil
}

// ============================================================================
// 2. PROTECTION SKIP — CUCKOO FILTER (cuckoo:user_reco:skips:{user_id})
// ============================================================================

// EnsureUserRecoSkipCuckooFilter vérifie la présence du Cuckoo Filter de l'utilisateur en L1.
// En cas de cache miss / éviction, reconstruit le filtre depuis MongoDB (user_reco_skips) de façon concurrent-safe.
func EnsureUserRecoSkipCuckooFilter(ctx context.Context, userID int64) error {
	if userID <= 0 {
		return nil
	}
	if redis.CuckooUserRecoSkips == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Le service de filtre de Cuckoo est indisponible.",
			fmt.Errorf("redis.CuckooUserRecoSkips collection is nil"),
		)
	}

	exists, err := redis.CuckooUserRecoSkips.Exists(ctx, userID)
	if err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de vérifier l'existence du filtre de Cuckoo.",
			err,
		)
	}
	if exists {
		return nil
	}

	// Protection contre les reconstructions concurrentes via un verrou éphémère court
	rebuildLockKey := fmt.Sprintf("rebuild:%d", userID)
	if redis.LockUserRecoCompute != nil {
		locked, errLock := redis.LockUserRecoCompute.SetNX(ctx, rebuildLockKey, 1, 10*time.Second)
		if errLock == nil && locked {
			defer func() {
				_ = redis.LockUserRecoCompute.DeleteObject(ctx, rebuildLockKey)
			}()
		}
	}

	// Réservation du Cuckoo Filter avec la capacité attendue N = 3000 (ε = 0.01)
	if errReserve := redis.CuckooUserRecoSkips.CFReserve(
		ctx,
		userID,
		variables.UserRecoCuckooCapacity,
	); errReserve != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'initialiser le filtre de Cuckoo pour l'utilisateur.",
			errReserve,
		)
	}

	// Chargement des skips actifs (< 7 jours) depuis MongoDB L2 si la collection est initialisée
	if mongo.UserRecoSkips != nil && mongo.UserRecoSkips.DB != nil {
		activeSkips, errMongo := mongo.MongoLoadActiveUserRecoSkips(ctx, userID)
		if errMongo != nil {
			numan_log.Warn(ctx).Err(errMongo).Int64("user_id", userID).Msg("Avertissement lors du chargement L2 des skips pour reconstruction du Cuckoo Filter")
		} else if len(activeSkips) > 0 {
			if len(activeSkips) > int(variables.UserRecoCuckooCapacity) {
				activeSkips = activeSkips[:variables.UserRecoCuckooCapacity]
			}
			items := make([]any, len(activeSkips))
			for i, skippedID := range activeSkips {
				items[i] = skippedID
			}
			if errAdd := redis.CuckooUserRecoSkips.CFAddItems(ctx, userID, items); errAdd != nil {
				numan_log.Warn(ctx).Err(errAdd).Int64("user_id", userID).Msg("Capacité atteinte ou erreur partielle lors de la réhydratation du Cuckoo Filter")
			}
		}
	}

	_ = redis.CuckooUserRecoSkips.RefreshTTL(ctx, userID)
	return nil
}

// AddUserRecoSkipToCuckoo insère de manière synchrone un utilisateur ignoré dans le Cuckoo Filter L1
// et le retire immédiatement des listes ZSET de recommandation pré-calculées.
func AddUserRecoSkipToCuckoo(ctx context.Context, userID int64, skippedUserID int64) error {
	if userID <= 0 || skippedUserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Identifiants utilisateurs invalides pour le skip.", nil)
	}
	if redis.CuckooUserRecoSkips == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Le service de filtre de Cuckoo est indisponible.",
			fmt.Errorf("redis.CuckooUserRecoSkips collection is nil"),
		)
	}

	if errEnsure := EnsureUserRecoSkipCuckooFilter(ctx, userID); errEnsure != nil {
		return errEnsure
	}

	if errAdd := redis.CuckooUserRecoSkips.CFAddItems(ctx, userID, []any{skippedUserID}); errAdd != nil {
		// Gestion de la saturation de capacité : réinitialisation/reconstruction propre du filtre
		numan_log.Warn(ctx).Err(errAdd).Int64("user_id", userID).Int64("skipped_user_id", skippedUserID).Msg("Saturation ou erreur sur le Cuckoo Filter, tentative de réallocation")
		_ = redis.CuckooUserRecoSkips.DeleteObject(ctx, userID)
		if errReserve := redis.CuckooUserRecoSkips.CFReserve(
			ctx,
			userID,
			variables.UserRecoCuckooCapacity,
		); errReserve != nil {
			return numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeCacheError,
				"Impossible de réinitialiser le filtre de Cuckoo après saturation.",
				errReserve,
			)
		}
		if errRetry := redis.CuckooUserRecoSkips.CFAddItems(ctx, userID, []any{skippedUserID}); errRetry != nil {
			return numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeCacheError,
				"Impossible d'enregistrer l'exclusion dans le filtre de Cuckoo.",
				errRetry,
			)
		}
	}

	_ = redis.CuckooUserRecoSkips.RefreshTTL(ctx, userID)

	// Retrait immédiat des listes L1 pré-calculées pour que l'utilisateur ignoré disparaisse instantanément
	skippedMember := strconv.FormatInt(skippedUserID, 10)
	recoLists := []*redis.Collection{
		redis.UserRecoListGlobal,
		redis.UserRecoListGeo,
		redis.UserRecoListAff,
		redis.UserRecoListTaste,
	}
	for _, listColl := range recoLists {
		if listColl != nil {
			_ = listColl.ZRem(ctx, userID, skippedMember)
		}
	}

	return nil
}

// FilterOutSkippedCandidates exclut tous les candidats présents (ou faux-positifs) dans le Cuckoo Filter de l'utilisateur.
// Si le filtre n'existe pas en L1, le reconstruit d'abord depuis MongoDB L2.
func FilterOutSkippedCandidates(ctx context.Context, userID int64, candidates []int64) ([]int64, error) {
	if len(candidates) == 0 {
		return []int64{}, nil
	}
	if err := EnsureUserRecoSkipCuckooFilter(ctx, userID); err != nil {
		return nil, err
	}

	existsMap, err := redis.CuckooUserRecoSkips.CFExistsMany(ctx, userID, candidates)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de vérifier les candidats dans le filtre de Cuckoo.",
			err,
		)
	}

	filtered := make([]int64, 0, len(candidates))
	for _, cID := range candidates {
		if existsMap[cID] {
			continue // Candidat déjà vu/ignoré (ou faux-positif Cuckoo -> exclu conformément à la spécification)
		}
		filtered = append(filtered, cID)
	}
	return filtered, nil
}

// ============================================================================
// 3. VERROU DE CALCUL ÉPHÉMÈRE (lock:user_reco:compute:{user_id})
// ============================================================================

// AcquireUserRecoComputeLock tente d'acquérir de manière atomique (SET NX + TTL ~12h) le verrou de calcul.
func AcquireUserRecoComputeLock(ctx context.Context, userID int64) (bool, error) {
	if userID <= 0 {
		return false, nil
	}
	if redis.LockUserRecoCompute == nil {
		return false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Le service de verrouillage de calcul est indisponible.",
			fmt.Errorf("redis.LockUserRecoCompute collection is nil"),
		)
	}

	acquired, err := redis.LockUserRecoCompute.SetNX(ctx, userID, domain.NowMillis(), variables.UserRecoComputeLockTTL)
	if err != nil {
		return false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'acquérir le verrou de calcul de recommandation.",
			err,
		)
	}
	return acquired, nil
}

// HasUserRecoComputeLock vérifie si un verrou de calcul est actuellement actif pour l'utilisateur.
func HasUserRecoComputeLock(ctx context.Context, userID int64) (bool, error) {
	if userID <= 0 {
		return false, nil
	}
	if redis.LockUserRecoCompute == nil {
		return false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Le service de verrouillage de calcul est indisponible.",
			fmt.Errorf("redis.LockUserRecoCompute collection is nil"),
		)
	}

	exists, err := redis.LockUserRecoCompute.Exists(ctx, userID)
	if err != nil {
		return false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de vérifier le verrou de calcul de recommandation.",
			err,
		)
	}
	return exists, nil
}

// ============================================================================
// 4. LECTURE ET PUBLICATION ATOMIQUE DES ZSETS DE RECOMMANDATION
// ============================================================================

// GetUserRecommendationIDs lit les identifiants d'utilisateurs recommandés depuis le ZSET Redis demandé.
// Retourne (ids, true, nil) en cas de Cache Hit, ou ([]int64{}, false, nil) en cas de Cache Miss.
func GetUserRecommendationIDs(ctx context.Context, userID int64, sortMode string, limit int64) ([]int64, bool, error) {
	if userID <= 0 {
		return []int64{}, false, nil
	}
	coll, err := resolveUserRecoCollection(sortMode)
	if err != nil {
		return nil, false, err
	}
	if coll == nil {
		return nil, false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"La collection de recommandation Redis n'est pas initialisée.",
			fmt.Errorf("recommendation redis collection for sort %s is nil", sortMode),
		)
	}

	exists, errExists := coll.Exists(ctx, userID)
	if errExists != nil {
		return nil, false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de vérifier la présence de la liste de recommandations en cache.",
			errExists,
		)
	}
	if !exists {
		return []int64{}, false, nil
	}

	if limit <= 0 {
		limit = variables.UserRecoDefaultLimit
	}
	if limit > variables.UserRecoMaxLimit {
		limit = variables.UserRecoMaxLimit
	}

	rawMembers, errRange := coll.ZRevRange(ctx, userID, 0, limit-1)
	if errRange != nil {
		return nil, false, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de lire la liste de recommandations depuis Redis.",
			errRange,
		)
	}

	validMembers := make([]string, 0, len(rawMembers))
	for _, m := range rawMembers {
		if m != variables.SpeedCacheEmptyMarker && m != variables.UserTimelineEmptyMarker {
			validMembers = append(validMembers, m)
		}
	}

	ids := pkg.ParseInt64List(validMembers)
	if ids == nil {
		ids = []int64{}
	}
	return ids, true, nil
}

// toRedisZSlice convertit une tranche de UserRecoScoredEntry en []*goredis.Z (tronquée à UserRecoTopK).
func toRedisZSlice(entries []UserRecoScoredEntry) []*goredis.Z {
	maxItems := variables.UserRecoTopK
	if maxItems <= 0 {
		maxItems = 150
	}
	if len(entries) > maxItems {
		entries = entries[:maxItems]
	}
	zSlice := make([]*goredis.Z, 0, len(entries))
	for _, e := range entries {
		if e.UserID <= 0 {
			continue
		}
		zSlice = append(zSlice, &goredis.Z{
			Score:  e.Score,
			Member: strconv.FormatInt(e.UserID, 10),
		})
	}
	if len(zSlice) == 0 {
		zSlice = append(zSlice, &goredis.Z{
			Score:  0,
			Member: variables.SpeedCacheEmptyMarker,
		})
	}
	return zSlice
}

// PublishUserRecommendationLists publie de manière atomique les 4 listes de recommandations (geo, aff, taste, global) dans Redis.
func PublishUserRecommendationLists(
	ctx context.Context,
	userID int64,
	geoList []UserRecoScoredEntry,
	affList []UserRecoScoredEntry,
	tasteList []UserRecoScoredEntry,
	globalList []UserRecoScoredEntry,
) error {
	if userID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Identifiant utilisateur invalide pour la publication des recommandations.", nil)
	}
	if redis.UserRecoListGeo == nil || redis.UserRecoListAff == nil || redis.UserRecoListTaste == nil || redis.UserRecoListGlobal == nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Les collections de recommandation Redis ne sont pas initialisées.",
			fmt.Errorf("one or more UserRecoList collections are nil"),
		)
	}

	ttl := variables.UserRecoListTTL
	if ttl <= 0 {
		ttl = 48 * time.Hour
	}

	publications := []struct {
		coll    *redis.Collection
		entries []UserRecoScoredEntry
	}{
		{coll: redis.UserRecoListGeo, entries: geoList},
		{coll: redis.UserRecoListAff, entries: affList},
		{coll: redis.UserRecoListTaste, entries: tasteList},
		{coll: redis.UserRecoListGlobal, entries: globalList},
	}

	for _, pub := range publications {
		zMembers := toRedisZSlice(pub.entries)
		if err := pub.coll.ZSetReplaceAtomic(ctx, userID, zMembers, ttl); err != nil {
			return numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeCacheError,
				"Impossible de publier atomiquement la liste de recommandations dans Redis.",
				err,
			)
		}
	}

	return nil
}
