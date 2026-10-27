package media_service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : ACTIVATION / DÉSACTIVATION DES MÉDIAS (MODE FANTÔME & QUARANTAINE)
// ############################################################################

// ActivateMediaBatch valide et sort une liste de médias de leur état "fantôme" (Out-of-Band).
// Si le TrustScore du propriétaire est < 0.40, le média est placé en Quarantaine (Visibility = 2).
func ActivateMediaBatch(ctx context.Context, mediaIDs []int64, ownerID int64) error {
	if len(mediaIDs) == 0 {
		return nil
	}

	currentTime := time.Now().UTC()

	// Évaluation O(1) RAM du palier de confiance de l'utilisateur
	targetVisibility := variables.MediaVisibilityPublic
	if ownerLite, errLite := cache_service.GetUserLite(ctx, ownerID); errLite == nil {
		if ownerLite.TrustScore > 0 && ownerLite.TrustScore < variables.TrustThresholdStandard {
			targetVisibility = variables.MediaVisibilityQuarantine
		}
	}

	for _, mediaID := range mediaIDs {
		if mediaID <= 0 {
			continue
		}

		// ── ÉTAPE 1 : RÉCUPÉRATION DU MÉDIA (CASCADE) ───────────────────────
		mediaPayload, errCascade := getMediaCascade(ctx, mediaID)
		if errCascade != nil {
			return errCascade
		}

		// ── ÉTAPE 2 : SÉCURITÉ ZERO-TRUST ───────────────────────────────────
		if mediaPayload.OwnerID != ownerID {
			return numan_error.NewForbidden(numan_error.CodeForbidden, "Tentative d'utilisation d'un média qui ne vous appartient pas.", nil)
		}
		if mediaPayload.Visibility == variables.MediaVisibilityRejected {
			return numan_error.NewForbidden(numan_error.CodeMediaRejected, "Ce média a été rejeté par la modération.", nil)
		}

		// ── ÉTAPE 3 : ACTIVATION OU MAINTIEN EN QUARANTAINE ─────────────────
		if mediaPayload.Visibility == variables.MediaVisibilityQuarantine {
			mediaPayload.Visibility = variables.MediaVisibilityQuarantine
		} else {
			mediaPayload.Visibility = targetVisibility
		}
		mediaPayload.UpdatedAt = domain.TimeToMillis(currentTime)

		// ── ÉTAPE 4 : MISE À JOUR SYNCHRONE (L1) ────────────────────────────
		_ = object_cache_service.SetMediaInObjectCache(ctx, mediaPayload)

		// ── ÉTAPE 5 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────
		errQueue := redis.EnqueueDB(ctx, mediaID, ownerID, redis.EntityMedia, redis.ActionUpdate, mediaPayload, redis.TargetAll)
		if errQueue != nil {
			numan_log.Error(ctx).Err(errQueue).Int64("media_id", mediaID).Msg("Échec du Write-Behind lors de l'activation du média")
			return numan_error.NewInternal()
		}
	}

	return nil
}

// DeactivateMediaBatch repasse une liste de médias à l'état de fantôme (Soft Delete).
// Le Garbage Collector (Worker) viendra les balayer physiquement et en BDD plus tard.
func DeactivateMediaBatch(ctx context.Context, mediaIDs []int64, ownerID int64) error {
	if len(mediaIDs) == 0 {
		return nil
	}

	currentTime := time.Now().UTC()

	for _, mediaID := range mediaIDs {
		if mediaID <= 0 {
			continue
		}

		// ── ÉTAPE 1 : RÉCUPÉRATION DU MÉDIA (CASCADE) ───────────────────────
		mediaPayload, errCascade := getMediaCascade(ctx, mediaID)
		if errCascade != nil {
			return errCascade
		}

		// ── ÉTAPE 2 : SÉCURITÉ ZERO-TRUST ───────────────────────────────────
		if mediaPayload.OwnerID != ownerID {
			return numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé : tentative de suppression d'un média qui ne vous appartient pas.", nil)
		}

		// ── ÉTAPE 3 : DÉSACTIVATION (SOFT DELETE = -1) ──────────────────────
		mediaPayload.Visibility = variables.MediaVisibilitySoftDeleted
		mediaPayload.UpdatedAt = domain.TimeToMillis(currentTime)

		// ── ÉTAPE 4 : MISE À JOUR SYNCHRONE (L1) ────────────────────────────
		_ = object_cache_service.SetMediaInObjectCache(ctx, mediaPayload)

		// ── ÉTAPE 5 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────
		errQueue := redis.EnqueueDB(ctx, mediaID, ownerID, redis.EntityMedia, redis.ActionUpdate, mediaPayload, redis.TargetAll)
		if errQueue != nil {
			numan_log.Error(ctx).Err(errQueue).Int64("media_id", mediaID).Msg("Échec du Write-Behind lors de la désactivation du média")
			return numan_error.NewInternal()
		}
	}

	return nil
}

// ActivateMediaBatchWithMaxIAScore active un lot de médias (1 à 4 images d'un Post) et retourne Score_IA_Post = MAX(P_ia).
func ActivateMediaBatchWithMaxIAScore(ctx context.Context, mediaIDs []int64, ownerID int64) (float64, error) {
	if err := ActivateMediaBatch(ctx, mediaIDs, ownerID); err != nil {
		return 0.0, err
	}
	maxIAScore := 0.0
	for _, mediaID := range mediaIDs {
		if mediaID <= 0 {
			continue
		}
		if mediaPayload, err := getMediaCascade(ctx, mediaID); err == nil {
			if mediaPayload.IAScore > maxIAScore {
				maxIAScore = mediaPayload.IAScore
			}
		}
	}
	return maxIAScore, nil
}

// ActivateMediaForMessage active un média attaché à un message privé/groupe (Visibility = 1, sans blocage quarantaine)
// et retourne son score IA NSFWJS (P_ia_img) pour l'évaluation du signalement automatique.
func ActivateMediaForMessage(ctx context.Context, mediaID int64, ownerID int64) (float64, error) {
	if mediaID <= 0 {
		return 0.0, nil
	}
	mediaPayload, errCascade := getMediaCascade(ctx, mediaID)
	if errCascade != nil {
		return 0.0, errCascade
	}
	if mediaPayload.OwnerID != ownerID {
		return 0.0, numan_error.NewForbidden(numan_error.CodeForbidden, "Tentative d'utilisation d'un média qui ne vous appartient pas.", nil)
	}
	if mediaPayload.Visibility == variables.MediaVisibilityRejected {
		return 0.0, numan_error.NewForbidden(numan_error.CodeMediaRejected, "Ce média a été rejeté par la modération.", nil)
	}

	// Pour la messagerie, le média n'est pas bloqué en quarantaine (Visibility = 1),
	// mais son score IA est retourné pour déclencher un signalement automatique si nécessaire.
	mediaPayload.Visibility = variables.MediaVisibilityPublic
	mediaPayload.UpdatedAt = domain.NowMillis()

	_ = object_cache_service.SetMediaInObjectCache(ctx, mediaPayload)
	if errQueue := redis.EnqueueDB(ctx, mediaID, ownerID, redis.EntityMedia, redis.ActionUpdate, mediaPayload, redis.TargetAll); errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("media_id", mediaID).Msg("Échec du Write-Behind lors de l'activation du média de messagerie")
		return 0.0, numan_error.NewInternal()
	}

	return mediaPayload.IAScore, nil
}
