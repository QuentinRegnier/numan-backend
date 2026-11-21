package cache_service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"go.mongodb.org/mongo-driver/bson"
)

// ############################################################################
// # MOTEUR DE RECOMMANDATION ET CLASSEMENTS (MOST CACHE)
// ############################################################################

// computeMediaCount extrait le nombre réel de médias attachés à une publication.
func computeMediaCount(postPayload post_models.PostPayload) int {
	if len(postPayload.MediaIDs) > 0 {
		return len(postPayload.MediaIDs)
	}
	if postPayload.HasMedia {
		return 1
	}
	return 0
}

// ComputeBoundedPriorityMultiplier calcule un multiplicateur de priorité borné [1.0, 2.5]
// appliqué une seule fois dans le pipeline MOST Cache (évite la double multiplication x45).
func ComputeBoundedPriorityMultiplier(priorityLevel int) float64 {
	switch {
	case priorityLevel <= 0:
		return 1.0
	case priorityLevel == 1:
		return 1.25 // Certifié
	case priorityLevel == 2:
		return 1.75 // Partenaire
	case priorityLevel == 3:
		return 2.0 // Modérateur
	default:
		return 2.5 // Admin (borné)
	}
}

// TrackAndCountAuthorPostsInWindow enregistre la publication dans la fenêtre glissante 24h de l'auteur
// et retourne le nombre de publications de cet auteur sur les dernières 24 heures (anti-flood).
func TrackAndCountAuthorPostsInWindow(ctx context.Context, authorID int64, postID int64, createdAt time.Time) int {
	if authorID <= 0 || redis.AuthorPostWindows == nil || redis.AuthorPostWindows.Client == nil {
		return 1
	}
	now := domain.NowTime()
	cutoffMs := domain.TimeToMillis(now.Add(-variables.AuthorPostWindowTTL))
	createdMs := domain.TimeToMillis(createdAt)
	postIDStr := strconv.FormatInt(postID, 10)

	_ = redis.AuthorPostWindows.ZRemRangeByScore(ctx, authorID, "-inf", strconv.FormatInt(cutoffMs, 10))
	if createdMs >= cutoffMs {
		_ = redis.AuthorPostWindows.ZAdd(ctx, authorID, float64(createdMs), postIDStr)
		_ = redis.AuthorPostWindows.Expire(ctx, authorID, variables.AuthorPostWindowTTL)
	}
	count, err := redis.AuthorPostWindows.ZCard(ctx, authorID)
	if err != nil || count < 1 {
		return 1
	}
	return int(count)
}

// UpdatePostRecommendationScore recalcule le score à partir de l'entité déjà chargée en RAM.
func UpdatePostRecommendationScore(ctx context.Context, postPayload post_models.PostPayload) {
	if postPayload.Visibility != variables.PostVisibilityAll {
		EvictPostFromPublicTrends(ctx, postPayload)
		return
	}

	UpdateScoreWithMetrics(
		ctx,
		postPayload.ID,
		postPayload.LikeCount,
		postPayload.CommentCount,
		postPayload.ViewCount,
		computeMediaCount(postPayload),
		domain.MillisToTime(postPayload.CreatedAt),
		postPayload.Hashtags,
		postPayload.IndirectHashtags,
		postPayload.Visibility,
		postPayload.ReportCount,
		postPayload.PriorityLevel,
		postPayload.Lang,
		int(postPayload.UserID),
	)
}

// EvaluatePostAfterLike force l'insertion du post avec sa valeur absolue dans les classements stricts.
func EvaluatePostAfterLike(ctx context.Context, postPayload post_models.PostPayload) {
	if postPayload.Visibility != variables.PostVisibilityAll {
		EvictPostFromPublicTrends(ctx, postPayload)
		return
	}

	normLang := variables.NormalizeLangForCache(postPayload.Lang)
	postIDStr := strconv.FormatInt(postPayload.ID, 10)
	if redis.RankedPosts != nil && redis.RankedPosts.Client != nil {
		errRedis := redis.RankedPosts.ZAddWithCap(ctx, variables.StrictRankSubKey(variables.StrictTypeLikes, normLang), float64(postPayload.LikeCount), postIDStr, variables.MaxStrictElements)
		if errRedis != nil {
			numan_log.Warn(ctx).Err(errRedis).Int64("post_id", postPayload.ID).Msg("Impossible de mettre à jour le classement strict des likes par langue")
		}
		_ = redis.RankedPosts.ZAddWithCap(ctx, "strict:likes", float64(postPayload.LikeCount), postIDStr, variables.MaxStrictElements)
	}
	UpdatePostRecommendationScore(ctx, postPayload)
}

// EvaluatePostAfterView force l'insertion du post avec sa valeur absolue dans les classements stricts.
func EvaluatePostAfterView(ctx context.Context, postPayload post_models.PostPayload) {
	if postPayload.Visibility != variables.PostVisibilityAll {
		EvictPostFromPublicTrends(ctx, postPayload)
		return
	}

	normLang := variables.NormalizeLangForCache(postPayload.Lang)
	postIDStr := strconv.FormatInt(postPayload.ID, 10)
	if redis.RankedPosts != nil && redis.RankedPosts.Client != nil {
		errRedis := redis.RankedPosts.ZAddWithCap(ctx, variables.StrictRankSubKey(variables.StrictTypeViews, normLang), float64(postPayload.ViewCount), postIDStr, variables.MaxStrictElements)
		if errRedis != nil {
			numan_log.Warn(ctx).Err(errRedis).Int64("post_id", postPayload.ID).Msg("Impossible de mettre à jour le classement strict des vues par langue")
		}
		_ = redis.RankedPosts.ZAddWithCap(ctx, "strict:views", float64(postPayload.ViewCount), postIDStr, variables.MaxStrictElements)
	}
	UpdatePostRecommendationScore(ctx, postPayload)
}

// ############################################################################
// # LECTURE ET FALLBACKS (L1 -> L2 -> L3)
// ############################################################################

// GetRankedPosts récupère les classements stricts avec auto-bascule sur Mongo/Postgres si hors RAM.
// Paramètre optionnel lang ...int : filtre le magasin linguistique correspondant.
func GetRankedPosts(ctx context.Context, rankType string, offset int64, limit int64, lang ...int) ([]post_models.PostPayload, error) {
	hasLang := len(lang) > 0 && variables.IsSupportedLanguage(lang[0])
	resolvedLang := variables.DefaultContentLanguage
	if hasLang {
		resolvedLang = lang[0]
	}
	cleanType := variables.NormalizeStrictRankType(rankType)

	// Si on dépasse la capacité RAM (MaxStrictElements), on interroge les bases de données froides.
	if offset >= int64(variables.MaxStrictElements) {
		mongoFilter := map[string]any{
			"visibility": variables.PostVisibilityAll,
		}
		if hasLang {
			mongoFilter["lang"] = resolvedLang
		}

		var mongoSort bson.D
		switch cleanType {
		case variables.StrictTypeLikes:
			mongoSort = bson.D{{Key: "like_count", Value: -1}, {Key: "created_at", Value: -1}}
		case variables.StrictTypeViews:
			mongoSort = bson.D{{Key: "view_count", Value: -1}, {Key: "created_at", Value: -1}}
		default:
			mongoSort = bson.D{{Key: "created_at", Value: -1}}
		}

		// TENTATIVE L2 (MongoDB)
		mongoDocuments, errMongo := mongo.Posts.GetPaginated(mongoFilter, mongoSort, offset, limit)
		if errMongo != nil {
			numan_log.Warn(ctx).Err(errMongo).Str("rank_type", rankType).Msg("Échec L2 lors de la récupération paginée des publications classées")
			// FALLBACK L3 (PostgreSQL)
			if hasLang {
				return getPostsFromPostgresPaginated(ctx, rankType, offset, limit, resolvedLang)
			}
			return getPostsFromPostgresPaginated(ctx, rankType, offset, limit)
		}

		var hydratedPosts []post_models.PostPayload
		for _, document := range mongoDocuments {
			var postPayload post_models.PostPayload
			if errStruct := pkg.ToStruct(document, &postPayload); errStruct == nil {
				if postPayload.Visibility == variables.PostVisibilityAll {
					hydratedPosts = append(hydratedPosts, postPayload)
				}
			} else {
				numan_log.Warn(ctx).Err(errStruct).Msg("Échec de conversion BSON vers PostPayload dans GetRankedPosts")
			}
		}
		return hydratedPosts, nil
	}

	// TENTATIVE L1 (RAM) : Lecture du ZSET partitionné par langue, avec fallback legacy si non spécifié
	if hasLang {
		posts, err := fetchAndHydrateFromCollection(ctx, redis.RankedPosts, variables.StrictRankSubKey(cleanType, resolvedLang), offset, limit)
		if err == nil && len(posts) > 0 {
			return posts, nil
		}
	}

	// Lecture sur la partition par défaut ou la sous-clé legacy ("strict:likes", "strict:views", "strict:recent")
	legacySubKey := strings.TrimPrefix(rankType, "most_cache:")
	posts, err := fetchAndHydrateFromCollection(ctx, redis.RankedPosts, legacySubKey, offset, limit)
	if err == nil && len(posts) > 0 {
		return posts, nil
	}
	return fetchAndHydrateFromCollection(ctx, redis.RankedPosts, variables.StrictRankSubKey(cleanType, resolvedLang), offset, limit)
}

// GetTagPosts récupère la timeline d'un hashtag précis (uniquement publications publiques visibility == 0).
// Paramètre optionnel lang ...int : filtre par langue si fourni.
func GetTagPosts(ctx context.Context, slug string, offset int64, limit int64, lang ...int) ([]post_models.PostPayload, error) {
	hasLang := len(lang) > 0 && variables.IsSupportedLanguage(lang[0])
	resolvedLang := variables.DefaultContentLanguage
	if hasLang {
		resolvedLang = lang[0]
	}
	canonicalSlug := service.ResolveCanonicalTag(ctx, slug, resolvedLang)
	if canonicalSlug == "" {
		canonicalSlug = slug
	}

	// Si on dépasse la capacité RAM
	if offset >= int64(variables.MaxTagElements) {
		var (
			postsFromMongo []post_models.PostPayload
			errMongo       error
		)
		if hasLang {
			postsFromMongo, errMongo = getPostsFromMongoPaginated("hashtags", canonicalSlug, offset, limit, resolvedLang)
		} else {
			postsFromMongo, errMongo = getPostsFromMongoPaginated("hashtags", canonicalSlug, offset, limit)
		}
		if errMongo != nil {
			numan_log.Warn(ctx).Err(errMongo).Str("slug", canonicalSlug).Msg("Échec L2 lors de la récupération paginée des publications par tag")
			// FALLBACK L3 (PostgreSQL) - Pur DDD
			var (
				pgIDs []int64
				errPg error
			)
			if hasLang {
				pgIDs, errPg = postgres.FuncLoadPostIDsByTagPaginated(ctx, canonicalSlug, offset, limit, resolvedLang)
			} else {
				pgIDs, errPg = postgres.FuncLoadPostIDsByTagPaginated(ctx, canonicalSlug, offset, limit)
			}
			if errPg != nil {
				return []post_models.PostPayload{}, errPg
			}
			return object_cache_service.GetPostsView(ctx, pgIDs)
		}
		return postsFromMongo, nil
	}

	if hasLang {
		posts, err := fetchAndHydrateFromCollection(ctx, redis.TagPosts, variables.TagIndexSubKey(resolvedLang, canonicalSlug), offset, limit)
		if err == nil && len(posts) > 0 {
			return posts, nil
		}
	}
	posts, err := fetchAndHydrateFromCollection(ctx, redis.TagPosts, canonicalSlug, offset, limit)
	if err == nil && len(posts) > 0 {
		return posts, nil
	}
	return fetchAndHydrateFromCollection(ctx, redis.TagPosts, variables.TagIndexSubKey(resolvedLang, canonicalSlug), offset, limit)
}

// updateTrendZSETs distribue le score de tendance global dans les différents rayons Redis partitionnés par langue.
// Note architecturale :
// - Les membres des ZSETs sont systématiquement des postID en ASCII décimal (compatibles ParseInt64List / ZREM / ZSCORE / ZUNIONSTORE).
// - Le poids des tags indirects (0.65) est directement appliqué au score lors de l'insertion dans TrendTagDaily/Weekly.
// - HashtagLeaderboard n'est PLUS écrasé ici : il est géré exclusivement par algo_trend_worker.go.
func updateTrendZSETs(
	ctx context.Context,
	postID int64,
	score float64,
	createdAt time.Time,
	directTags []string,
	indirectTags []string,
	bucketDate string,
	bucketHour string,
	bucketWeek string,
	lang int,
	includeHourly bool,
	includeDaily bool,
	includeWeekly bool,
) error {
	normLang := variables.NormalizeLangForCache(lang)
	postIDStr := strconv.FormatInt(postID, 10)
	createdAtMs := float64(domain.TimeToMillis(createdAt))

	// 1. Buckets Globaux par langue (+ sous-clé legacy pour compatibilité ascendante)
	if includeHourly {
		hourlySubKey := variables.TrendGlobalHourlySubKey(normLang, bucketHour)
		if err := redis.TrendGlobalHourly.ZAddWithCap(ctx, hourlySubKey, score, postIDStr, variables.TDDMaxZSET); err != nil {
			return numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeCacheError,
				"Impossible de mettre à jour la tendance horaire dans le cache.",
				err,
			)
		}
		_ = redis.TrendGlobalHourly.ZAddWithCap(ctx, bucketHour, score, postIDStr, variables.TDDMaxZSET)
	}

	if includeDaily {
		dailySubKey := variables.TrendGlobalDailySubKey(normLang, bucketDate)
		if err := redis.TrendGlobalDaily.ZAddWithCap(ctx, dailySubKey, score, postIDStr, variables.TDDMaxZSET); err != nil {
			return numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeCacheError,
				"Impossible de mettre à jour la tendance journalière dans le cache.",
				err,
			)
		}
		_ = redis.TrendGlobalDaily.ZAddWithCap(ctx, bucketDate, score, postIDStr, variables.TDDMaxZSET)
	}

	// 2. Buckets par Tags (Directs = 100% du score, Indirects = 65% du score)
	processTags := func(tagsList []string, isIndirect bool) error {
		tagScore := score
		if isIndirect {
			tagScore = score * variables.IndirectTagScoreWeight
		}

		for _, hashtag := range tagsList {
			cleanSlug, isFound := service.GetTagFromKeywordWithLang(ctx, hashtag, normLang, 0)
			if !isFound || cleanSlug == "" {
				continue
			}

			// Indexation dans redis.TagPosts pour l'UI et la recherche par tag
			if redis.TagPosts != nil && redis.TagPosts.Client != nil {
				_ = redis.TagPosts.ZAddWithCap(ctx, variables.TagIndexSubKey(normLang, cleanSlug), createdAtMs, postIDStr, variables.MaxPostsPerTagInRAM)
				_ = redis.TagPosts.ZAddWithCap(ctx, cleanSlug, createdAtMs, postIDStr, variables.MaxPostsPerTagInRAM)
			}

			if includeDaily {
				tagDailyKey := variables.TrendTagDailySubKey(normLang, cleanSlug, bucketDate)
				if err := redis.TrendTagDaily.ZAddWithCap(ctx, tagDailyKey, tagScore, postIDStr, variables.TDDMaxZSET); err != nil {
					appErr := numan_error.NewAppError(
						http.StatusInternalServerError,
						numan_error.CodeCacheError,
						"Impossible de mettre à jour la tendance quotidienne du tag dans le cache.",
						err,
					)
					numan_log.Error(ctx).Err(appErr).Str("tag", cleanSlug).Msg("Échec de l'ajout à la tendance quotidienne")
					return appErr
				}
				_ = redis.TrendTagDaily.ZAddWithCap(ctx, fmt.Sprintf("%s:%s", cleanSlug, bucketDate), tagScore, postIDStr, variables.TDDMaxZSET)
			}

			if includeWeekly {
				tagWeeklyKey := variables.TrendTagWeeklySubKey(normLang, cleanSlug, bucketWeek)
				if err := redis.TrendTagWeekly.ZAddWithCap(ctx, tagWeeklyKey, tagScore, postIDStr, variables.TDDMaxZSET); err != nil {
					appErr := numan_error.NewAppError(
						http.StatusInternalServerError,
						numan_error.CodeCacheError,
						"Impossible de mettre à jour la tendance hebdomadaire du tag dans le cache.",
						err,
					)
					numan_log.Error(ctx).Err(appErr).Str("tag", cleanSlug).Msg("Échec de l'ajout à la tendance hebdomadaire")
					return appErr
				}
				_ = redis.TrendTagWeekly.ZAddWithCap(ctx, fmt.Sprintf("%s:%s", cleanSlug, bucketWeek), tagScore, postIDStr, variables.TDDMaxZSET)
			}
		}
		return nil
	}

	if err := processTags(directTags, false); err != nil {
		return err
	}
	if err := processTags(indirectTags, true); err != nil {
		return err
	}

	return nil
}

// UpdateScoreWithMetrics orchestre le calcul mathématique et la distribution des scores.
// Paramètres optionnels extraParams ...int :
//   - extraParams[0] : code langue du post (lang)
//   - extraParams[1] : identifiant de l'auteur (authorID) pour charger AuthorGrade, AuthorTrustScore et AuthorPostsInWindow
func UpdateScoreWithMetrics(
	ctx context.Context,
	postID int64,
	likesCount int,
	commentsCount int,
	viewsCount int,
	mediaCount int,
	createdAt time.Time,
	directTags []string,
	indirectTags []string,
	visibility int,
	reportCount int,
	priorityLevel int,
	extraParams ...int,
) {
	// P0 ISOLATION DE VISIBILITÉ STRICTE :
	// Seules les publications strictement publiques (visibility == 0 / PostVisibilityAll)
	// peuvent intégrer les classements et tendances publics du Magasin.
	if visibility != variables.PostVisibilityAll {
		return
	}

	lang := variables.DefaultContentLanguage
	if len(extraParams) > 0 {
		lang = variables.NormalizeLangForCache(extraParams[0])
	}
	var authorID int64
	if len(extraParams) > 1 && extraParams[1] > 0 {
		authorID = int64(extraParams[1])
	}

	// Chargement des métriques de confiance auteur (AuthorGrade, AuthorTrustScore, AuthorPostsInWindow)
	authorGrade := variables.UserGradeUser
	authorTrustScore := 50.0
	authorPostsInWindow := 1
	if authorID > 0 {
		if userLite, errLite := GetUserLite(ctx, authorID); errLite == nil {
			authorGrade = userLite.Grade
			if userLite.TrustScore > 0 {
				authorTrustScore = float64(userLite.TrustScore)
			}
		}
		authorPostsInWindow = TrackAndCountAuthorPostsInWindow(ctx, authorID, postID, createdAt)
	}

	now := domain.NowTime()
	ageDuration := now.Sub(createdAt)
	if ageDuration < 0 {
		ageDuration = 0
	}
	ageInSeconds := ageDuration.Seconds()

	baseOptions := service.ScoreOptions{
		LikesCount:          likesCount,
		CommentsCount:       commentsCount,
		ViewCount:           viewsCount,
		MediaCount:          mediaCount,
		ReportCount:         reportCount,
		AgeSeconds:          ageInSeconds,
		AuthorGrade:         authorGrade,
		AuthorTrustScore:    authorTrustScore,
		AuthorPostsInWindow: authorPostsInWindow,
		IsDeleted:           false,
	}

	globalScore := service.CalculateRecommendationScore(baseOptions)
	globalScore *= ComputeBoundedPriorityMultiplier(priorityLevel)

	postIDStr := strconv.FormatInt(postID, 10)
	scoreStrictRecent := float64(domain.TimeToMillis(createdAt))

	if redis.RankedPosts != nil && redis.RankedPosts.Client != nil {
		if err := redis.RankedPosts.ZAddWithCap(ctx, variables.StrictRankSubKey(variables.StrictTypeRecent, lang), scoreStrictRecent, postIDStr, variables.MaxStrictElements); err != nil {
			appErr := numan_error.NewAppError(
				http.StatusInternalServerError,
				numan_error.CodeCacheError,
				"Impossible d'ajouter la publication à la liste des éléments récents.",
				err,
			)
			numan_log.Error(ctx).Err(appErr).Msg("Échec de l'ajout à la liste des éléments récents")
			return
		}
		_ = redis.RankedPosts.ZAddWithCap(ctx, "strict:recent", scoreStrictRecent, postIDStr, variables.MaxStrictElements)
	}

	// Buckets déterministes basés sur la date de création du post (et non NowTime())
	// avec garde-fous temporels pour que le Time-Decay Tier 4 (>7j) ne réinjecte jamais un vieux post dans le bucket du jour/heure courant.
	bucketTime := createdAt.UTC()
	if createdAt.IsZero() {
		bucketTime = now.UTC()
	}
	year, isoWeek := bucketTime.ISOWeek()
	weekString := fmt.Sprintf("%d-W%02d", year, isoWeek)
	dateString := bucketTime.Format("20060102")
	hourString := bucketTime.Format("2006010215")

	includeHourly := ageDuration <= variables.TrendGlobalHourlyTTL
	includeDaily := ageDuration <= variables.TrendGlobalDailyTTL
	includeWeekly := ageDuration <= variables.TrendTagWeeklyTTL

	if !includeHourly && !includeDaily && !includeWeekly {
		return
	}

	err := updateTrendZSETs(
		ctx,
		postID,
		globalScore,
		createdAt,
		directTags,
		indirectTags,
		dateString,
		hourString,
		weekString,
		lang,
		includeHourly,
		includeDaily,
		includeWeekly,
	)
	if err != nil {
		numan_log.Error(ctx).Err(err).Int64("post_id", postID).Msg("Échec de la mise à jour des tendances")
		return
	}
}

// EvictPostFromPublicTrends purge intégralement une publication de tous les ZSETs publics du Magasin
// (TrendGlobalHourly, TrendGlobalDaily, TrendTagDaily, TrendTagWeekly, RankedPosts strict:likes/views/recent, TagPosts)
// lors d'une suppression, d'une mise en quarantaine, d'un changement de visibilité (0 -> 1/2/3/<0) ou d'un changement de langue.
func EvictPostFromPublicTrends(ctx context.Context, post post_models.PostPayload) {
	postIDStr := strconv.FormatInt(post.ID, 10)
	normLang := variables.NormalizeLangForCache(post.Lang)

	// Collecte des timestamps déterministes (date de création du post + instant courant en filet de sécurité)
	timestamps := []time.Time{domain.NowTime().UTC()}
	if post.CreatedAt > 0 {
		timestamps = append(timestamps, domain.MillisToTime(post.CreatedAt).UTC())
	}

	// Collecte de toutes les variantes des tags directs et indirects (bruts, normalisés, canoniques)
	tagSet := make(map[string]struct{})
	addTagVariants := func(raw string) {
		rawTrim := strings.TrimSpace(raw)
		if rawTrim == "" {
			return
		}
		tagSet[rawTrim] = struct{}{}
		norm := service.NormalizeHashtag(ctx, rawTrim)
		if norm != "" {
			tagSet[norm] = struct{}{}
		}
		canon := service.ResolveCanonicalTag(ctx, rawTrim, normLang)
		if canon != "" {
			tagSet[canon] = struct{}{}
		}
	}
	for _, t := range post.Hashtags {
		addTagVariants(t)
	}
	for _, t := range post.IndirectHashtags {
		addTagVariants(t)
	}

	// 1. Purge des classements stricts (par langue + legacy)
	if redis.RankedPosts != nil && redis.RankedPosts.Client != nil {
		for _, strictType := range []string{variables.StrictTypeLikes, variables.StrictTypeViews, variables.StrictTypeRecent} {
			_ = redis.RankedPosts.ZRem(ctx, variables.StrictRankSubKey(strictType, normLang), postIDStr)
			_ = redis.RankedPosts.ZRem(ctx, "strict:"+strictType, postIDStr)
		}
	}

	// 2. Purge des index de tags (TagPosts)
	if redis.TagPosts != nil && redis.TagPosts.Client != nil {
		for tag := range tagSet {
			_ = redis.TagPosts.ZRem(ctx, variables.TagIndexSubKey(normLang, tag), postIDStr)
			_ = redis.TagPosts.ZRem(ctx, tag, postIDStr)
		}
	}

	// 3. Purge des buckets temporels globaux et par tag
	for _, ts := range timestamps {
		dateStr := ts.Format("20060102")
		hourStr := ts.Format("2006010215")
		year, isoWeek := ts.ISOWeek()
		weekStr := fmt.Sprintf("%d-W%02d", year, isoWeek)

		if redis.TrendGlobalHourly != nil && redis.TrendGlobalHourly.Client != nil {
			_ = redis.TrendGlobalHourly.ZRem(ctx, variables.TrendGlobalHourlySubKey(normLang, hourStr), postIDStr)
			_ = redis.TrendGlobalHourly.ZRem(ctx, hourStr, postIDStr)
		}
		if redis.TrendGlobalDaily != nil && redis.TrendGlobalDaily.Client != nil {
			_ = redis.TrendGlobalDaily.ZRem(ctx, variables.TrendGlobalDailySubKey(normLang, dateStr), postIDStr)
			_ = redis.TrendGlobalDaily.ZRem(ctx, dateStr, postIDStr)
		}

		for tag := range tagSet {
			if redis.TrendTagDaily != nil && redis.TrendTagDaily.Client != nil {
				_ = redis.TrendTagDaily.ZRem(ctx, variables.TrendTagDailySubKey(normLang, tag, dateStr), postIDStr)
				_ = redis.TrendTagDaily.ZRem(ctx, fmt.Sprintf("%s:%s", tag, dateStr), postIDStr)
			}
			if redis.TrendTagWeekly != nil && redis.TrendTagWeekly.Client != nil {
				_ = redis.TrendTagWeekly.ZRem(ctx, variables.TrendTagWeeklySubKey(normLang, tag, weekStr), postIDStr)
				_ = redis.TrendTagWeekly.ZRem(ctx, fmt.Sprintf("%s:%s", tag, weekStr), postIDStr)
			}
		}
	}
}
