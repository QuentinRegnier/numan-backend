package like_service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/like_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/notification_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : AIMER OU DÉSAIMER UN COMMENTAIRE (TOGGLE)
// ############################################################################

// ToggleCommentLike agit comme un routeur hybride : Tri synchrone en RAM + Persistance asynchrone.
func ToggleCommentLike(ctx context.Context, callerID int64, input like_models.LikeCommentInput) error {

	// ── ÉTAPE 1 : IDEMPOTENCE EN RAM (O(1)) ─────────────────────────────────
	// Bloque le Spam Clic instantanément pour protéger les Workers

	if input.Action == "like" {
		if !cache_service.TryAddLikeIdempotency(ctx, variables.LikeTargetTypeComment, input.CommentID, callerID) {
			return nil
		}
	} else {
		if !cache_service.TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypeComment, input.CommentID, callerID) {
			return nil
		}
	}

	// ── ÉTAPE 2 : RÉCUPÉRATION DU COMMENTAIRE (CASCADE L1->L2->L3) ──────────

	commentPayload, errCascade := getCommentCascade(ctx, input.CommentID)
	if errCascade != nil || commentPayload.Visibility == variables.CommentVisibilitySoftDelete {
		// Le commentaire a été supprimé, on annule l'idempotence et on rejette
		_ = cache_service.TryRemoveLikeIdempotency(ctx, variables.LikeTargetTypeComment, input.CommentID, callerID)
		return errCascade // getCommentCascade renvoie déjà un numan_error propre
	}

	// ── ÉTAPE 3 : MISE À JOUR SYNCHRONE (ZSET + RAM L1) ─────────────────────

	deltaValue := 1.0
	redisActionType := redis.ActionCreate

	if input.Action == "unlike" {
		deltaValue = -1.0
		redisActionType = redis.ActionDelete
	}

	// Incrémentation immédiate de l'objet en L1
	commentPayload.LikeCount += int(deltaValue)
	if commentPayload.LikeCount < 0 {
		commentPayload.LikeCount = 0
	}
	commentPayload.Score += int(deltaValue)
	_ = object_cache_service.SetCommentInObjectCache(ctx, commentPayload)

	// Vérification opportuniste du Post Parent pour mettre à jour son classement (Tri à bulle)
	if object_cache_service.IsPostInObjectCache(ctx, commentPayload.PostID) {
		_ = object_cache_service.IncrementCommentScoreInZSET(ctx, commentPayload.PostID, commentPayload.ID, deltaValue)
	}

	// ── ÉTAPE 4 : DÉLÉGATION DE LA PERSISTANCE AUX WORKERS (WRITE-BEHIND) ───

	likeRecordPayload := like_models.LikePayload{
		ID:         pkg.GenerateID(),
		TargetType: variables.LikeTargetTypeComment,
		TargetID:   input.CommentID,
		UserID:     callerID,
		CreatedAt:  domain.NowTime().Format(time.RFC3339),
	}

	errQueue := redis.EnqueueDB(ctx, likeRecordPayload.ID, commentPayload.PostID, redis.EntityLike, redisActionType, likeRecordPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("comment_id", input.CommentID).Msg("Échec du Write-Behind pour ToggleCommentLike")
		return nil // On ne fait pas crasher l'UX pour une perte de like
	}

	// ── ÉTAPE 5 : NOTIFICATION TEMPS RÉEL ───────────────────────────────────

	if input.Action == "like" && commentPayload.UserID != callerID {
		go func() {
			errNotif := notification_service.DispatchNotification(context.Background(), commentPayload.UserID, callerID, variables.EventCommentLiked, commentPayload.ID)
			if errNotif != nil {
				numan_log.Error(ctx).Err(errNotif).Int64("comment_id", commentPayload.ID).Msg("Échec de l'envoi de la notification pour un like de commentaire")
			}
		}()
	}

	return nil
}
