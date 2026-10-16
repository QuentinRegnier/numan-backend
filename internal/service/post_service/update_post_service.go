package post_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/algorithm_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : MISE À JOUR D'UNE PUBLICATION (POST)
// ############################################################################

// UpdatePost gère la modification des métadonnées et du contenu d'un post en récupérant l'objet complet pour nourrir le Bulk Update des workers.
func UpdatePost(ctx context.Context, callerID int64, input post_models.UpdatePostInput) error {
	// ── ÉTAPE 0 : VÉRIFICATION DU NIVEAU DE VISIBILITÉ ──────────────────────
	if input.Visibility < variables.PostVisibilityAll || input.Visibility > variables.PostVisibilityFriend {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Niveau de visibilité non supporté.", nil)
	}

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS ZERO-TRUST ET RÉCUPÉRATION DE L'OBJET ────
	postPayload, errSecurity := security_service.LeftPost(ctx, input.PostID, callerID)
	if errSecurity != nil {
		numan_log.Error(ctx).Err(errSecurity).Int64("post_id", input.PostID).Int64("user_id", callerID).Msg("Échec de la récupération sécurisée du post")
		return numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : APPLICATION DES MODIFICATIONS MÉTIER ──────────────────────
	if len(input.Hashtags) > 20 {
		input.Hashtags = input.Hashtags[:20]
	}
	if len(input.Identifiers) > 10 {
		input.Identifiers = input.Identifiers[:10]
	}

	postPayload.Content = pkg.CleanStr(input.Content)
	postPayload.Hashtags = pkg.SliceUniqueStr(input.Hashtags)
	postPayload.Identifiers = pkg.SliceUniqueInt64(input.Identifiers)
	postPayload.Location = pkg.CleanStr(input.Location)
	postPayload.Visibility = input.Visibility
	postPayload.UpdatedAt = domain.NowMillis()

	// L'IA locale (Edge Computing) saura qu'il faut recalculer ses affinités grâce à l'incrémentation.
	postPayload.VectorVersion += 1

	// ── ÉTAPE 3 : RE-VECTORISATION SYNCHRONE DU CONTENU ─────────────────────
	// On met à jour le vecteur avec les nouvelles données textuelles/thématiques.
	postPayload.Vector = algorithm_service.ComputeContentVectorFull(ctx, postPayload, nil)

	// ── ÉTAPE 4 : SAUVEGARDE ET DÉLÉGATION AUX WORKERS BATCH ────────────────

	// 1. Écrasement LFU immédiat en RAM
	errCache := object_cache_service.SetPostInObjectCache(ctx, postPayload)
	if errCache != nil {
		numan_log.Warn(ctx).Err(errCache).Int64("post_id", postPayload.ID).Msg("Échec de la mise à jour du post dans le cache L1")
	}

	// 2. Envoi de l'objet COMPLET dans la file asynchrone pour que les workers BulkUpdate fonctionnent
	errQueue := redis.EnqueueDB(ctx, postPayload.ID, 0, redis.EntityPost, redis.ActionUpdate, postPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("post_id", postPayload.ID).Msg("Échec du Write-Behind pour la mise à jour du post")
		return numan_error.NewInternal()
	}

	return nil
}
