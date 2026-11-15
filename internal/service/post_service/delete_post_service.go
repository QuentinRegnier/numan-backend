package post_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/algorithm_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : RÉTRACTATION D'UNE PUBLICATION (SOFT DELETE)
// ############################################################################

func DeletePost(ctx context.Context, callerID int64, input post_models.DeletePostInput) error {
	if err := input.Validate(); err != nil {
		return err
	}

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS ZERO-TRUST ET RÉCUPÉRATION DU POST ───────
	postPayload, errSecurity := security_service.LeftPost(ctx, input.PostID, callerID)
	if errSecurity != nil {
		return errSecurity
	}

	// ── ÉTAPE 2 : MUTATION SYNCHRONE DES CACHES (BOUCLIER TOMBSTONE L1) ─────

	// A. Tombstone Pattern : On garde le post en RAM mais on le marque supprimé
	// Cela protège Mongo/Postgres des requêtes fantômes (Cache Hit positif sur un objet mort).
	postPayload.Visibility = variables.PostVisibilityDeleted // (ex: -1)
	postPayload.UpdatedAt = domain.NowMillis()
	_ = object_cache_service.SetPostInObjectCache(ctx, postPayload)

	// B. Purge de la Timeline Utilisateur (Le post disparaît du profil)
	_ = cache_service.RemovePostFromUserProfile(ctx, postPayload.UserID, input.PostID)

	// C. Purge des Commentaires en RAM associés à ce post (ZSET + JSON L1)
	object_cache_service.PurgePostCommentsFromL1(ctx, input.PostID)

	// D. Suppression Algorithmique (LSH)
	_ = algorithm_service.PurgePostVectors(ctx, input.PostID)

	// E. Purge absolue des Médias associés en RAM (Eux ne sont pas requis en Tombstone)
	for _, mediaID := range postPayload.MediaIDs {
		_ = object_cache_service.DeleteMediaFromObjectCache(ctx, mediaID)
	}

	// ── ÉTAPE 3 : DÉLÉGATION DE LA PERSISTANCE (CASCADE BDD WRITE-BEHIND) ───
	errQueue := redis.EnqueueDB(ctx, postPayload.ID, 0, redis.EntityPost, redis.ActionDelete, postPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("post_id", input.PostID).Msg("Échec du Write-Behind lors de la suppression d'un post")
		return numan_error.NewInternal(errQueue)
	}

	return nil
}
