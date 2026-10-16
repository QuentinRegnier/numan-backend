package like_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/like_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : RÉCUPÉRATION DES ABONNÉS AYANT LIKÉ UN POST
// ############################################################################

// GetPostLikes récupère les abonnés ayant liké un post en appliquant les règles de visibilité.
func GetPostLikes(ctx context.Context, input like_models.GetPostLikesInput) (like_models.GetPostLikesOutput, error) {
	var err_offset, err_limit numan_error.Error
	input.Offset, err_offset, input.Limit, err_limit = pkg.BatchVerif(input.Offset, input.Limit)
	if err_offset != nil || err_limit != nil {
		return like_models.GetPostLikesOutput{}, numan_error.Combine(err_offset, err_limit)
	}

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS AU POST (CASCADE L1 -> L2 -> L3) ─────────

	var postPayload post_models.PostPayload
	var isPostFound bool

	if cachedPost, errCache := object_cache_service.GetPostFromObjectCache(ctx, input.PostID); errCache == nil {
		postPayload = cachedPost
		isPostFound = true
	} else {
		// TENTATIVE L2 (MongoDB - Warm Storage)
		postsFromMongo, errMongo := mongo.MongoLoadPosts(ctx, []int64{input.PostID})
		if errMongo == nil && len(postsFromMongo) > 0 {
			postPayload = postsFromMongo[0]
			isPostFound = true

			go func(p post_models.PostPayload) {
				_ = object_cache_service.SetPostInObjectCache(context.Background(), p)
			}(postPayload)

		} else {
			// TENTATIVE L3 (PostgreSQL - Cold Storage)
			postsFromPostgres, errPg := postgres.FuncLoadPosts(ctx, []int64{input.PostID}, 1, 0)
			if errPg != nil {
				numan_log.Error(ctx).Err(errPg).Int64("post_id", input.PostID).Msg("Erreur L3 lors de la vérification du post pour GetPostLikes")
				return like_models.GetPostLikesOutput{}, numan_error.NewInternal()
			}

			if len(postsFromPostgres) > 0 {
				postPayload = postsFromPostgres[0]
				isPostFound = true

				_ = object_cache_service.SetPostInObjectCache(ctx, postPayload)

				go func(p post_models.PostPayload) {
					bgCtx := context.Background()
					_ = redis.EnqueueDB(bgCtx, p.ID, p.UserID, redis.EntityPost, redis.ActionUpdate, p, redis.TargetMongo)
				}(postPayload)
			}
		}
	}

	if !isPostFound || postPayload.Visibility == variables.PostVisibilityDeleted {
		return like_models.GetPostLikesOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Publication introuvable ou supprimée.", nil)
	}

	// Matrice de Confidentialité
	if postPayload.UserID != input.CallerID {
		relationState := cache_service.RelationValue(ctx, postPayload.UserID, input.CallerID)

		if relationState == variables.RelationStateBlocked {
			return like_models.GetPostLikesOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé.", nil)
		}
		if postPayload.Visibility == variables.PostVisibilitySubcriber && relationState < variables.RelationStateFollow { // Réservé aux Abonnés
			return like_models.GetPostLikesOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Liste des likes réservée aux abonnés de l'auteur.", nil)
		}
		if postPayload.Visibility == variables.PostVisibilityFriend && relationState != variables.RelationStateFriend { // Réservé aux Amis
			return like_models.GetPostLikesOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Liste des likes réservée aux amis de l'auteur.", nil)
		}
		if postPayload.Visibility < variables.PostVisibilityAll || postPayload.Visibility > variables.PostVisibilityFriend {
			return like_models.GetPostLikesOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé.", nil)
		}
	}

	// ── ÉTAPE 2 : RÉCUPÉRATION DES LIKES (L2 Mongo -> L3 Postgres) ──────────

	var userIDsThatLiked []int64

	// On tente Mongo (L2) d'abord
	userIDsThatLiked, errMongo := mongo.MongoGetPostLikes(ctx, input.PostID, input.Limit, input.Offset)

	// Si Mongo échoue ou ne renvoie rien (Cache miss), on tape Postgres (L3)
	if errMongo != nil || len(userIDsThatLiked) == 0 {
		likesFromPostgres, errPg := postgres.FuncLoadLikes(ctx, 0, input.PostID, 0, input.Limit, 0)
		if errPg != nil {
			numan_log.Error(ctx).Err(errPg).Int64("post_id", input.PostID).Msg("Erreur L3 lors de la récupération de la liste des likes")
			return like_models.GetPostLikesOutput{}, numan_error.NewInternal()
		}

		for _, likePayload := range likesFromPostgres {
			userIDsThatLiked = append(userIDsThatLiked, likePayload.UserID)

			go func(l like_models.LikePayload) {
				bgCtx := context.Background()
				_ = redis.EnqueueDB(bgCtx, l.ID, l.TargetID, redis.EntityLike, redis.ActionUpdate, l, redis.TargetMongo)
			}(likePayload)
		}
	}

	// ── ÉTAPE 3 : HYDRATATION EN MASSE (SPEED CACHE + DOMAINE MÉDIA) ────────

	if len(userIDsThatLiked) == 0 {
		return like_models.GetPostLikesOutput{
			PostID: input.PostID,
			Users:  make([]auth_models.UserLiteView, 0), // Prévention du `null` en JSON
		}, nil
	}

	var hydratedUsersList []auth_models.UserLiteView

	for _, targetUserID := range userIDsThatLiked {

		if userLiteData, errCache := cache_service.GetUserLite(ctx, targetUserID); errCache == nil {

			var userAvatarView media_models.MediaView
			if userLiteData.ProfilePictureID > 0 {
				if generatedView, errMedia := media_service.GenerateMediaViewCascade(ctx, userLiteData.ProfilePictureID, targetUserID, 0, input.CallerID); errMedia == nil {
					userAvatarView = generatedView
				}
			}

			hydratedUsersList = append(hydratedUsersList, auth_models.UserLiteView{
				User:     userLiteData,
				Avatar:   userAvatarView,
				IsOnline: cache_service.IsUserOnline(ctx, targetUserID),
			})
		}
	}

	return like_models.GetPostLikesOutput{
		PostID: input.PostID,
		Users:  hydratedUsersList,
	}, nil
}
