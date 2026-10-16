package comment_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : RÉCUPÉRATION DES COMMENTAIRES (CASCADE L1 -> L2 -> L3)
// ############################################################################

// GetComments est la fonction hybride pour récupérer les commentaires.
// Elle renvoie un tableau d'enveloppes (GetCommentOutput) pour isoler les erreurs partielles
// sans faire crasher la liste entière.
func GetComments(ctx context.Context, callerID int64, input comment_models.GetCommentsInput) ([]comment_models.GetCommentOutput, error) {
	var err_offset, err_limit *numan_error.AppError
	input.Offset, err_offset, input.Limit, err_limit = pkg.BatchVerif(input.Offset, input.Limit)
	if err_offset != nil || err_limit != nil {
		return nil, numan_error.Combine(err_offset, err_limit)
	}
	var finalResults []comment_models.GetCommentOutput

	// ── ÉTAPE 0 : VÉRIFICATION DES DROITS D'ACCÈS AU POST PARENT ────────────

	var postPayload post_models.PostPayload
	postPayload, errCache := object_cache_service.GetPostFromObjectCache(ctx, input.PostID)

	if errCache != nil {
		// FALLBACK L2 (MongoDB) : On cherche le post dans le stockage tiède
		postsFromMongo, errMongo := mongo.MongoLoadPosts(ctx, []int64{input.PostID})
		if errMongo == nil && len(postsFromMongo) > 0 {
			postPayload = postsFromMongo[0]
			_ = object_cache_service.SetPostInObjectCache(ctx, postPayload) // Hydratation L1
		} else {
			// FALLBACK ABSOLU L3 (PostgreSQL)
			postsFromPostgres, errPg := postgres.FuncLoadPosts(ctx, []int64{input.PostID}, 1, 0)
			if errPg != nil {
				numan_log.Error(ctx).Err(errPg).Int64("post_id", input.PostID).Msg("Erreur L3 lors de la vérification du post parent")
				return nil, numan_error.NewInternal()
			}
			if len(postsFromPostgres) == 0 {
				return nil, numan_error.NewNotFound(numan_error.CodeNotFound, "Le post parent est introuvable ou a été supprimé.", nil)
			}

			postPayload = postsFromPostgres[0]

			// AUTO-GUÉRISON EN CASCADE (L3 -> L2 -> L1)
			go func(p post_models.PostPayload) {
				bgCtx := context.Background()
				_ = object_cache_service.SetPostInObjectCache(bgCtx, p)
				_ = redis.EnqueueDB(bgCtx, p.ID, p.UserID, redis.EntityPost, redis.ActionUpdate, p, redis.TargetMongo)
			}(postPayload)
		}
	}

	// ⚡ MATRICE DE VISIBILITÉ EXACTE
	isAuthor := postPayload.UserID == callerID
	if !isAuthor {
		relationState := cache_service.RelationValue(ctx, postPayload.UserID, callerID)

		if postPayload.Visibility == variables.PostVisibilityDeleted || relationState == variables.RelationStateBlocked {
			return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé.", nil) // Bloqué ou Soft-Delete
		}
		if postPayload.Visibility == variables.PostVisibilitySubcriber && relationState < variables.RelationStateFollow {
			return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Les commentaires sont réservés aux abonnés de cet utilisateur.", nil)
		}
		if postPayload.Visibility == variables.PostVisibilityFriend && relationState != variables.RelationStateFriend {
			return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Les commentaires sont réservés aux amis de cet utilisateur.", nil)
		}
		if postPayload.Visibility < variables.PostVisibilityAll || postPayload.Visibility > variables.PostVisibilityFriend {
			return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé.", nil)
		}
	}

	// ── ÉTAPE 1 : TENTATIVE L1 (VIP PARKING ZSET REDIS) ─────────────────────

	// 🛡️ BOUCLIER DE PAGINATION
	if input.Limit <= 0 || input.Limit > 100 {
		input.Limit = 50
	}

	// On n'utilise le ZSET L1 que pour les premiers commentaires (Offset faible)
	if input.Offset < int64(variables.MaxZsetPostComment) && object_cache_service.IsPostInObjectCache(ctx, input.PostID) {
		commentIDs, _ := object_cache_service.GetTopCommentIDs(ctx, input.PostID, input.Offset, input.Limit)

		if len(commentIDs) > 0 {
			hydratedCommentsMap := fetchCommentsCascade(ctx, commentIDs)

			for _, id := range commentIDs {
				commentPayload, exists := hydratedCommentsMap[id]

				// Si introuvable en base ou Soft-Delete, on renvoie une erreur encapsulée locale
				if !exists || commentPayload.Visibility == -1 {
					finalResults = append(finalResults, comment_models.GetCommentOutput{
						CommentID: id,
						Error:     "Ce commentaire est introuvable ou a été supprimé.",
					})
				} else {
					finalResults = append(finalResults, hydrateCommentOutput(ctx, callerID, commentPayload))
				}
			}
			return finalResults, nil // 🚀 RETOUR INSTANTANÉ L1
		}
	}

	// ── ÉTAPE 2 : TENTATIVE L2 (MONGODB) ────────────────────────────────────

	commentsFromMongo, errMongo := mongo.MongoLoadCommentsPaginated(ctx, input.PostID, input.Offset, input.Limit)
	if errMongo == nil && len(commentsFromMongo) > 0 {
		for _, commentPayload := range commentsFromMongo {
			_ = object_cache_service.SetCommentInObjectCache(ctx, commentPayload)

			if input.Offset < int64(variables.MaxZsetPostComment) {
				_ = object_cache_service.AddCommentToZSET(ctx, commentPayload.PostID, commentPayload.ID, float64(commentPayload.Score))
			}
			finalResults = append(finalResults, hydrateCommentOutput(ctx, callerID, commentPayload))
		}
		return finalResults, nil // 🚀 RETOUR RAPIDE L2
	}

	// ── ÉTAPE 3 : TENTATIVE L3 (POSTGRESQL - SOURCE DE VÉRITÉ) ──────────────

	if input.Offset == 0 || input.AfterID != nil {
		commentsFromPostgres, errPg := postgres.FuncLoadCommentsKeyset(ctx, input.PostID, input.AfterScore, input.AfterCreatedAt, input.AfterID, input.Limit)
		if errPg != nil {
			numan_log.Error(ctx).Err(errPg).Int64("post_id", input.PostID).Msg("Erreur L3 lors de la récupération des commentaires")
			return nil, numan_error.NewInternal()
		}

		for _, commentPayload := range commentsFromPostgres {

			// AUTO-GUÉRISON L3 -> L2 & L1
			go func(c comment_models.CommentPayload) {
				bgCtx := context.Background()
				_ = object_cache_service.SetCommentInObjectCache(bgCtx, c)
				_ = redis.EnqueueDB(bgCtx, c.ID, c.PostID, redis.EntityComment, redis.ActionUpdate, c, redis.TargetMongo)
			}(commentPayload)

			_ = object_cache_service.AddCommentToZSET(ctx, commentPayload.PostID, commentPayload.ID, float64(commentPayload.Score))
			finalResults = append(finalResults, hydrateCommentOutput(ctx, callerID, commentPayload))
		}
		return finalResults, nil
	}

	// Aucun commentaire trouvé
	return []comment_models.GetCommentOutput{}, nil
}
