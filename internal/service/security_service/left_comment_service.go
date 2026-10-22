package security_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE DE SÉCURITÉ : DROITS DE PROPRIÉTÉ D'UN COMMENTAIRE
// ############################################################################

// LeftComment récupère un commentaire complet (L1 -> L2 -> L3) et vérifie
// que l'utilisateur appelant en est bien l'auteur légitime.
func LeftComment(ctx context.Context, commentID int64, userID int64) (comment_models.CommentPayload, error) {
	var commentPayload comment_models.CommentPayload
	var isCommentFound bool

	// ── ÉTAPE 1 : TENTATIVE L1 (OBJECT CACHE - LFU) ─────────────────────────

	if cachedComment, errCache := object_cache_service.GetCommentFromObjectCache(ctx, commentID); errCache == nil && cachedComment.ID != 0 {
		commentPayload = cachedComment
		isCommentFound = true
	} else {

		// ── ÉTAPE 2 : TENTATIVE L2 (MONGODB WARM STORAGE) ───────────────────

		mongoCommentsList, errMongo := mongo.MongoLoadComments(ctx, []int64{commentID})
		if errMongo == nil && len(mongoCommentsList) > 0 {
			commentPayload = mongoCommentsList[0]
			isCommentFound = true

			// AUTO-GUÉRISON L1 (Immédiat en RAM)
			_ = object_cache_service.SetCommentInObjectCache(ctx, commentPayload)

		} else {

			// ── ÉTAPE 3 : FALLBACK ABSOLU L3 (POSTGRESQL COLD STORAGE) ──────

			pgComment, errPg := postgres.FuncGetComment(ctx, commentID)
			if errPg != nil {
				numan_log.Error(ctx).Err(errPg).Int64("comment_id", commentID).Msg("Erreur L3 lors de la vérification de sécurité d'un commentaire")
				return comment_models.CommentPayload{}, errPg
			}

			if pgComment.ID != 0 {
				commentPayload = pgComment
				isCommentFound = true

				// AUTO-GUÉRISON L1 (Immédiat en RAM)
				_ = object_cache_service.SetCommentInObjectCache(ctx, commentPayload)

				// AUTO-GUÉRISON L2 (Asynchrone via Worker Mongo)
				go func(c comment_models.CommentPayload) {
					backgroundCtx := context.Background()
					// PartitionKey = PostID pour grouper contextuellement les commentaires
					_ = redis.EnqueueDB(backgroundCtx, c.ID, c.PostID, redis.EntityComment, redis.ActionUpdate, c, redis.TargetMongo)
				}(commentPayload)
			}
		}
	}

	// ── ÉTAPE 4 : VÉRIFICATION DES RÈGLES DE SÉCURITÉ ───────────────────────

	if !isCommentFound || commentPayload.Visibility == variables.CommentVisibilitySoftDelete {
		// Furtivité absolue (Soft Delete)
		return comment_models.CommentPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Commentaire introuvable ou supprimé.", nil)
	}

	if commentPayload.UserID != userID {
		return comment_models.CommentPayload{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous n'êtes pas autorisé à réaliser cette action sur ce commentaire.", nil)
	}

	return commentPayload, nil
}
