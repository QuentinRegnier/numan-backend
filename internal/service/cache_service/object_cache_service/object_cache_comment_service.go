package object_cache_service

import (
	"context"
	"net/http"
	"strconv"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : OBJECT CACHE (COMMENTAIRES LFU & ZSET DE TRI)
// ############################################################################

// GetCommentFromObjectCache récupère un commentaire depuis le cache L1.
func GetCommentFromObjectCache(ctx context.Context, commentID int64) (comment_models.CommentPayload, error) {
	var commentPayload comment_models.CommentPayload

	errRedis := redis.Comments.GetObject(ctx, commentID, &commentPayload)
	if errRedis != nil {
		return comment_models.CommentPayload{}, errRedis
	}

	return commentPayload, nil
}

// SetCommentInObjectCache enregistre ou met à jour un commentaire dans le cache L1.
func SetCommentInObjectCache(ctx context.Context, commentPayload comment_models.CommentPayload) error {
	errRedis := redis.Comments.SetObject(ctx, commentPayload.ID, commentPayload)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de sauvegarder le commentaire dans l'Object Cache.",
			errRedis,
		)
	}
	return nil
}

// DeleteCommentFromObjectCache purge instantanément un commentaire du cache L1.
func DeleteCommentFromObjectCache(ctx context.Context, commentID int64) error {
	errRedis := redis.Comments.DeleteObject(ctx, commentID)
	if errRedis != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de supprimer le commentaire de l'Object Cache.",
			errRedis,
		)
	}
	return nil
}

// ############################################################################
// # GESTION DU TRI HYBRIDE DES COMMENTAIRES (ZSET L1)
// ############################################################################

// GetTopCommentIDs récupère les IDs des meilleurs commentaires depuis le ZSET (O(log(N) + M)).
func GetTopCommentIDs(ctx context.Context, postID int64, paginationOffset int64, paginationLimit int64) ([]int64, error) {
	idStringsList, errRedis := redis.PostComments.ZRevRange(ctx, postID, paginationOffset, paginationOffset+paginationLimit-1)
	if errRedis != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de lire les identifiants de commentaires depuis le cache.",
			errRedis,
		)
	}

	parsedCommentIDsList := pkg.ParseInt64List(idStringsList)

	return parsedCommentIDsList, nil
}

// AddCommentToZSET ajoute un commentaire dans le ZSET associé à une publication, plafonné par configuration.
func AddCommentToZSET(ctx context.Context, postID int64, commentID int64, commentScore float64) error {
	commentIDString := strconv.FormatInt(commentID, 10)

	errZAdd := redis.PostComments.ZAddWithCap(ctx, postID, commentScore, commentIDString, variables.MaxZsetPostComment)
	if errZAdd != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'ajouter le commentaire au ZSET du cache.",
			errZAdd,
		)
	}

	_ = redis.PostComments.RefreshTTL(ctx, postID)
	return nil
}

// RemoveCommentFromZSET retire un commentaire du ZSET d'une publication.
func RemoveCommentFromZSET(ctx context.Context, postID int64, commentID int64) error {
	commentIDString := strconv.FormatInt(commentID, 10)

	errZRem := redis.PostComments.ZRem(ctx, postID, commentIDString)
	if errZRem != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de supprimer le commentaire du ZSET du cache.",
			errZRem,
		)
	}

	return nil
}

// IncrementCommentScoreInZSET applique un delta de score sur un commentaire dans le ZSET.
func IncrementCommentScoreInZSET(ctx context.Context, postID int64, commentID int64, scoreIncrement float64) error {
	commentIDString := strconv.FormatInt(commentID, 10)

	errIncr := redis.PostComments.ZIncrBy(ctx, postID, scoreIncrement, commentIDString)
	if errIncr != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible d'incrémenter le score du commentaire dans le ZSET du cache.",
			errIncr,
		)
	}

	_ = redis.PostComments.RefreshTTL(ctx, postID)
	return nil
}

// PurgePostCommentsFromL1 supprime à la fois tous les payloads de commentaires de l'Object Cache
// et atomise le ZSET de tri associé à la publication.
func PurgePostCommentsFromL1(ctx context.Context, postID int64) {
	idStringsList, errRedis := redis.PostComments.ZRevRange(ctx, postID, 0, -1)
	if errRedis == nil {
		parsedCommentIDsList := pkg.ParseInt64List(idStringsList)
		for _, parsedCommentID := range parsedCommentIDsList {
			_ = DeleteCommentFromObjectCache(ctx, parsedCommentID)
		}
	}

	// Atomisation complète du ZSET de la publication
	_ = redis.PostComments.DeleteObject(ctx, postID)
}
