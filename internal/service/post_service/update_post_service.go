package post_service

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/algorithm_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : MISE À JOUR D'UNE PUBLICATION (POST)
// ############################################################################

// UpdatePost gère la modification des métadonnées et du contenu d'un post en récupérant l'objet complet pour nourrir le Bulk Update des workers.
func UpdatePost(ctx context.Context, callerID int64, input post_models.UpdatePostInput) error {
	// ── ÉTAPE 0 : VÉRIFICATION DU PAYLOAD ───────────────────────────────────
	if err := input.Validate(); err != nil {
		return err
	}

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS ZERO-TRUST ET RÉCUPÉRATION DE L'OBJET ────
	postPayload, errSecurity := security_service.LeftPost(ctx, input.PostID, callerID)
	if errSecurity != nil {
		numan_log.Error(ctx).Err(errSecurity).Int64("post_id", input.PostID).Int64("user_id", callerID).Msg("Échec de la récupération sécurisée du post")
		return numan_error.NewInternal()
	}

	oldPostSnapshot := postPayload

	// ── ÉTAPE 2 : APPLICATION DES MODIFICATIONS MÉTIER ──────────────────────
	postPayload.Content = pkg.CleanStr(input.Content)

	// Préservation obligatoire de l'auto-tag auteur (user_{authorID}) lors de la mise à jour des hashtags
	updatedHashtags := pkg.SliceUniqueStr(input.Hashtags)
	authorHashtag := fmt.Sprintf("%s%d", variables.AuthorTagPrefix, postPayload.UserID)
	hasAuthorTag := false
	for _, h := range updatedHashtags {
		if h == authorHashtag {
			hasAuthorTag = true
			break
		}
	}
	if !hasAuthorTag && postPayload.UserID > 0 {
		updatedHashtags = append(updatedHashtags, authorHashtag)
	}
	postPayload.Hashtags = updatedHashtags

	postPayload.Identifiers = pkg.SliceUniqueInt64(input.Identifiers)
	postPayload.Location = pkg.CleanStr(input.Location)
	postPayload.Visibility = input.Visibility
	postPayload.UpdatedAt = domain.NowMillis()

	// Gestion de la langue lors d'une mise à jour :
	// - Si input.Lang ou input.LangISO est fourni, on valide et on applique la nouvelle langue.
	// - Sinon, on préserve la langue existante du post (ne jamais écraser silencieusement).
	if input.Lang != nil || strings.TrimSpace(input.LangISO) != "" {
		resolvedLang, errLang := variables.ResolveContentLanguage(input.Lang, input.LangISO, postPayload.Lang)
		if errLang != nil {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La langue de publication spécifiée n'est pas supportée.", errLang)
		}
		postPayload.Lang = resolvedLang
	}

	// L'IA locale (Edge Computing) saura qu'il faut recalculer ses affinités grâce à l'incrémentation.
	postPayload.VectorVersion += 1

	// ── ÉTAPE 3 : RE-VECTORISATION SYNCHRONE DU CONTENU ─────────────────────
	// On met à jour le vecteur avec les nouvelles données textuelles/thématiques.
	postPayload.Vector = algorithm_service.ComputeContentVectorFull(ctx, postPayload, nil)

	if err := postPayload.Validate(); err != nil {
		return err
	}

	// ── ÉTAPE 4 : INVALIDATION SYNCHRONE DES TENDANCES PUBLIQUES ────────────
	// Si le post était public (0) et devient restreint (1, 2, 3, <0), change de langue ou change de tags,
	// on l'évince immédiatement des anciens ZSETs publics de tendances/classements.
	if oldPostSnapshot.Visibility == variables.PostVisibilityAll {
		if postPayload.Visibility != variables.PostVisibilityAll || oldPostSnapshot.Lang != postPayload.Lang || !equalStringSlices(oldPostSnapshot.Hashtags, postPayload.Hashtags) {
			cache_service.EvictPostFromPublicTrends(ctx, oldPostSnapshot)
		}
	}

	// ── ÉTAPE 5 : SAUVEGARDE ET DÉLÉGATION AUX WORKERS BATCH ────────────────

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

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
