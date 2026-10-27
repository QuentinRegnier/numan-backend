package like_service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/like_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/notification_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : AIMER OU DÉSAIMER UN POST (TOGGLE)
// ############################################################################

// TogglePostLike agit comme un routeur asynchrone ultra-rapide (Fire and Forget).
func TogglePostLike(ctx context.Context, callerID int64, input like_models.LikePostInput) error {

	// ── ÉTAPE 1 : IDEMPOTENCE EN RAM (O(1)) ─────────────────────────────────

	if input.Action == "like" {
		if !cache_service.TryAddLikeIdempotency(ctx, variables.LikeTargetTypePost, input.PostID, callerID) {
			return nil
		}
	} else {
		if !cache_service.TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypePost, input.PostID, callerID) {
			return nil
		}
	}

	// ── ÉTAPE 2 : MISE À JOUR SYNCHRONE DU L1 (TEMPS RÉEL) ──────────────────

	deltaValue := 1
	redisActionType := redis.ActionCreate

	if input.Action == "unlike" {
		deltaValue = -1
		redisActionType = redis.ActionDelete
	}

	// Lecture opportuniste en L1
	var postAuthorID int64
	if postPayload, errCache := object_cache_service.GetPostFromObjectCache(ctx, input.PostID); errCache == nil {
		postAuthorID = postPayload.UserID
		postPayload.LikeCount += deltaValue

		if postPayload.LikeCount < 0 {
			postPayload.LikeCount = 0
		}
		_ = object_cache_service.SetPostInObjectCache(ctx, postPayload)

		// Routage intelligent vers l'IA de Classement (Most Cache)
		if input.Action == "like" {
			cache_service.EvaluatePostAfterLike(ctx, postPayload)
		}
	}

	// ── ÉTAPE 3 : DÉLÉGATION DE LA PERSISTANCE AUX WORKERS ──────────────────

	likeRecordPayload := like_models.LikePayload{
		ID:         pkg.GenerateID(),
		TargetType: variables.LikeTargetTypePost,
		TargetID:   input.PostID,
		UserID:     callerID,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}

	errQueue := redis.EnqueueDB(ctx, likeRecordPayload.ID, 0, redis.EntityLike, redisActionType, likeRecordPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("post_id", input.PostID).Msg("Échec du Write-Behind pour TogglePostLike")
		return nil // Non bloquant pour l'UX
	}

	// ── ÉTAPE 4 : NOTIFICATION TEMPS RÉEL & BONUS TRUST SCORE ──────────────

	if input.Action == "like" && postAuthorID != 0 && postAuthorID != callerID {
		// Bonus Trust Score si le contenu est aimé par un Modérateur ou Administrateur (§C.13 : +0.04)
		if callerLite, errLite := cache_service.GetUserLite(ctx, callerID); errLite == nil && callerLite.Grade >= variables.UserGradeModerator {
			_ = moderation_service.PublishTrustScoreEvent(
				ctx,
				postAuthorID,
				variables.TrustTriggerLikedByAdmin,
				input.PostID,
				"Publication aimée par un modérateur ou administrateur",
			)
		}

		go func(authorID int64) {
			errNotif := notification_service.DispatchNotification(context.Background(), authorID, callerID, variables.EventPostLiked, input.PostID)
			if errNotif != nil {
				numan_log.Error(ctx).Err(errNotif).Int64("post_id", input.PostID).Msg("Échec de l'envoi de la notification pour un like de post")
			}
		}(postAuthorID)
	}

	return nil
}
