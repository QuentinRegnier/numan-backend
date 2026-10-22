package cache_service

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
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
	"github.com/vmihailenco/msgpack/v5"
	"go.mongodb.org/mongo-driver/bson"
)

// ############################################################################
// # MOTEUR DE RECOMMANDATION ET CLASSEMENTS (MOST CACHE)
// ############################################################################

// UpdatePostRecommendationScore recalcule le score à partir de l'entité déjà chargée en RAM.
func UpdatePostRecommendationScore(ctx context.Context, postPayload post_models.PostPayload) {
	mediaCount := 0
	if postPayload.HasMedia || len(postPayload.MediaIDs) > 0 {
		mediaCount = 1
	}

	UpdateScoreWithMetrics(
		ctx,
		postPayload.ID,
		postPayload.LikeCount,
		postPayload.CommentCount,
		postPayload.ViewCount,
		mediaCount,
		domain.MillisToTime(postPayload.CreatedAt),
		postPayload.Hashtags,
		postPayload.IndirectHashtags,
		postPayload.Visibility,
		postPayload.ReportCount,
		postPayload.PriorityLevel,
	)
}

// EvaluatePostAfterLike force l'insertion du post avec sa valeur absolue dans les classements stricts.
func EvaluatePostAfterLike(ctx context.Context, postPayload post_models.PostPayload) {
	errRedis := redis.ZAddWithCap(ctx, variables.RedisKeyStrictLikes, float64(postPayload.LikeCount), postPayload.ID, variables.MaxStrictElements)
	if errRedis != nil {
		numan_log.Warn(ctx).Err(errRedis).Int64("post_id", postPayload.ID).Msg("Impossible de mettre à jour le classement strict des likes")
	}
	UpdatePostRecommendationScore(ctx, postPayload)
}

// EvaluatePostAfterView force l'insertion du post avec sa valeur absolue dans les classements stricts.
func EvaluatePostAfterView(ctx context.Context, postPayload post_models.PostPayload) {
	errRedis := redis.ZAddWithCap(ctx, variables.RedisKeyStrictViews, float64(postPayload.ViewCount), postPayload.ID, variables.MaxStrictElements)
	if errRedis != nil {
		numan_log.Warn(ctx).Err(errRedis).Int64("post_id", postPayload.ID).Msg("Impossible de mettre à jour le classement strict des vues")
	}
	UpdatePostRecommendationScore(ctx, postPayload)
}

// ############################################################################
// # LECTURE ET FALLBACKS (L1 -> L2 -> L3)
// ############################################################################

// GetRankedPosts récupère les classements stricts avec auto-bascule sur Mongo/Postgres si hors RAM.
func GetRankedPosts(ctx context.Context, rankType string, offset int64, limit int64) ([]post_models.PostPayload, error) {

	// Si on dépasse la capacité RAM (MaxStrictElements), on interroge les bases de données froides.
	if offset >= int64(variables.MaxStrictElements) {
		mongoFilter := map[string]any{}
		var mongoSort bson.D

		switch rankType {
		case "strict:likes":
			mongoSort = bson.D{{Key: "like_count", Value: -1}, {Key: "created_at", Value: -1}}
		case "strict:views":
			mongoSort = bson.D{{Key: "view_count", Value: -1}, {Key: "created_at", Value: -1}}
		case "strict:recent":
			mongoSort = bson.D{{Key: "created_at", Value: -1}}
		default:
			mongoSort = bson.D{{Key: "created_at", Value: -1}} // Fallback sécurisé
		}

		// TENTATIVE L2 (MongoDB)
		mongoDocuments, errMongo := mongo.Posts.GetPaginated(mongoFilter, mongoSort, offset, limit)
		if errMongo != nil {
			// FALLBACK L3 (PostgreSQL)
			return getPostsFromPostgresPaginated(ctx, rankType, offset, limit)
		}

		var hydratedPosts []post_models.PostPayload
		for _, document := range mongoDocuments {
			var postPayload post_models.PostPayload
			if errStruct := pkg.ToStruct(document, &postPayload); errStruct == nil {
				hydratedPosts = append(hydratedPosts, postPayload)
			}
		}
		return hydratedPosts, nil
	}

	// TENTATIVE L1 (RAM)
	return fetchAndHydrateFromCollection(ctx, redis.RankedPosts, rankType, offset, limit)
}

// GetTagPosts récupère la timeline d'un hashtag précis.
func GetTagPosts(ctx context.Context, slug string, offset int64, limit int64) ([]post_models.PostPayload, error) {

	// Si on dépasse la capacité RAM
	if offset >= int64(variables.MaxTagElements) {
		postsFromMongo, errMongo := getPostsFromMongoPaginated("hashtags", slug, offset, limit)
		if errMongo != nil {
			// FALLBACK L3 (PostgreSQL) - Pur DDD
			pgIDs, errPg := postgres.FuncLoadPostIDsByTagPaginated(ctx, slug, offset, limit)
			if errPg != nil {
				return []post_models.PostPayload{}, errPg
			}
			return object_cache_service.GetPostsView(ctx, pgIDs)
		}
		return postsFromMongo, nil
	}

	return fetchAndHydrateFromCollection(ctx, redis.TagPosts, slug, offset, limit)
}

// updateTrendZSETs distribue le score de tendance global dans les différents rayons Redis.
func updateTrendZSETs(ctx context.Context, postID int64, score float64, directTags []string, indirectTags []string, currentDate string, currentHour string, currentWeek string) error {

	// 1. Buckets Globaux
	if err := redis.TrendGlobalHourly.ZAddWithCap(ctx, currentHour, score, postID, variables.TDDMaxZSET); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de mettre à jour la tendance horaire dans le cache.",
			err,
		)
	}

	if err := redis.TrendGlobalDaily.ZAddWithCap(ctx, currentDate, score, postID, variables.TDDMaxZSET); err != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de mettre à jour la tendance journalière dans le cache.",
			err,
		)
	}

	// 2. Buckets par Tags (Sérialisation Msgpack pour le flag IsIndirect)
	processTags := func(tagsList []string, isIndirect bool) {
		for _, hashtag := range tagsList {
			if cleanSlug, isFound := service.GetTagFromKeyword(ctx, hashtag); isFound {

				tagItem := lite_models.TagPostItem{PostID: postID, IsIndirect: isIndirect}
				binaryData, err := msgpack.Marshal(tagItem)
				if err != nil {
					numan_log.Error(ctx).Err(err).Msg("Échec de la sérialisation MessagePack pour un item de tag")
					return
				}
				memberBinaryString := string(binaryData)

				err = redis.TrendTagDaily.ZAddWithCap(ctx, fmt.Sprintf("%s:%s", cleanSlug, currentDate), score, memberBinaryString, variables.TDDMaxZSET)
				if err != nil {
					numan_log.Error(ctx).Err(err).Msg("Échec de l'ajout à la tendance quotidienne")
					return
				}
				err = redis.TrendTagWeekly.ZAddWithCap(ctx, fmt.Sprintf("%s:%s", cleanSlug, currentWeek), score, memberBinaryString, variables.TDDMaxZSET)
				if err != nil {
					numan_log.Error(ctx).Err(err).Msg("Échec de l'ajout à la tendance hebdomadaire")
					return
				}

				// Leaderboard ne stocke que le slug (chaîne de caractères)
				err = redis.HashtagLeaderboard.ZAddWithCap(ctx, "global", score, cleanSlug, variables.TDDMaxZSET)
				if err != nil {
					numan_log.Error(ctx).Err(err).Msg("Échec de l'ajout au leaderboard des hashtags")
					return
				}
			}
		}
	}

	processTags(directTags, false)
	processTags(indirectTags, true)

	return nil
}

// UpdateScoreWithMetrics orchestre le calcul mathématique et la distribution des scores.
func UpdateScoreWithMetrics(ctx context.Context, postID int64, likesCount int, commentsCount int, viewsCount int, mediaCount int, createdAt time.Time, directTags []string, indirectTags []string, visibility int, reportCount int, priorityLevel int) {

	ageInSeconds := time.Since(createdAt).Seconds()

	baseOptions := service.ScoreOptions{
		LikesCount:    likesCount,
		CommentsCount: commentsCount,
		ViewCount:     viewsCount,
		MediaCount:    mediaCount,
		ReportCount:   reportCount,
		AgeSeconds:    ageInSeconds,
		IsDeleted:     visibility == 0,
	}

	globalScore := service.CalculateRecommendationScore(baseOptions)

	priorityMultiplier := 1.0
	switch priorityLevel {
	case 1:
		priorityMultiplier = 1.25 // Certifié
	case 2:
		priorityMultiplier = 2.0 // Partenaire
	case 3:
		priorityMultiplier = 3.0 // Modérateur
	case 4:
		priorityMultiplier = 15.0 // Admin
	}
	globalScore *= priorityMultiplier

	scoreStrictRecent := float64(createdAt.UnixMilli())
	err := redis.ZAddWithCap(ctx, variables.RedisKeyStrictRecent, scoreStrictRecent, postID, variables.MaxStrictElements)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Échec de l'ajout à la liste des éléments récents")
		return
	}

	currentTime := time.Now().UTC()
	year, isoWeek := currentTime.ISOWeek()
	weekString := fmt.Sprintf("%d-W%02d", year, isoWeek)

	err = updateTrendZSETs(ctx, postID, globalScore, directTags, indirectTags, currentTime.Format("20060102"), currentTime.Format("2006010215"), weekString)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Échec de la mise à jour des tendances")
		return
	}
}
