package media_service

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	infraPostgres "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/vmihailenco/msgpack/v5"
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

	currentTime := domain.NowTime()

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

		if err := mediaPayload.Validate(); err != nil {
			return err
		}

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

	currentTime := domain.NowTime()

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

// ############################################################################
// # SERVICE : ACTIVATION / DÉSACTIVATION DES ASSETS D'ARTICLES (CROSS-ADMIN)
// ############################################################################

// ValidateAndActivateAdminArticleMedia valide la sémantique et le préfixe `articles/` de tous les médias d'un article
// (couverture optionnelle, document Markdown obligatoire, pièces jointes optionnelles), autorise l'usage cross-admin
// exclusivement pour les assets sous `articles/`, interdit tout média privé utilisateur (`users/...`),
// et active chaque média en `MediaVisibilityPublic` (`1`) sans jamais appliquer de quarantaine.
func ValidateAndActivateAdminArticleMedia(
	ctx context.Context,
	coverMediaID int64,
	markdownMediaID int64,
	attachmentMediaIDs []int64,
) error {
	if markdownMediaID <= 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'identifiant du document Markdown (markdown_media_id) est obligatoire.",
			nil,
		)
	}

	type roleRequirement struct {
		mediaID    int64
		isMarkdown bool
		roleLabel  string
	}

	checks := make([]roleRequirement, 0, len(attachmentMediaIDs)+2)
	checks = append(checks, roleRequirement{
		mediaID:    markdownMediaID,
		isMarkdown: true,
		roleLabel:  "markdown_media_id",
	})
	if coverMediaID > 0 {
		checks = append(checks, roleRequirement{
			mediaID:    coverMediaID,
			isMarkdown: false,
			roleLabel:  "cover_media_id",
		})
	}
	for _, attID := range attachmentMediaIDs {
		if attID <= 0 {
			continue
		}
		checks = append(checks, roleRequirement{
			mediaID:    attID,
			isMarkdown: false,
			roleLabel:  "attachment_media_ids",
		})
	}

	// Étape 1 : Validation complète avant toute mutation (tout-ou-rien)
	resolvedPayloads := make(map[int64]media_models.MediaPayload, len(checks))
	for _, chk := range checks {
		mp, exists := resolvedPayloads[chk.mediaID]
		if !exists {
			var errCascade error
			mp, errCascade = getMediaCascade(ctx, chk.mediaID)
			if errCascade != nil {
				return numan_error.NewBadRequest(
					numan_error.CodeInvalidPayload,
					fmt.Sprintf("Le média d'article (%s = %d) est introuvable ou a été supprimé.", chk.roleLabel, chk.mediaID),
					errCascade,
				)
			}
			resolvedPayloads[chk.mediaID] = mp
		}

		cleanPath := strings.TrimPrefix(strings.TrimSpace(mp.StoragePath), "/")
		if !strings.HasPrefix(cleanPath, variables.ArticleMediaStoragePrefix) {
			return numan_error.NewForbidden(
				numan_error.CodeForbidden,
				"Accès refusé : seuls les assets publics d'articles (préfixe 'articles/') peuvent être rattachés à un article.",
				fmt.Errorf("media %d has non-article storage_path %q", chk.mediaID, mp.StoragePath),
			)
		}

		if mp.Visibility == variables.MediaVisibilityRejected {
			return numan_error.NewForbidden(
				numan_error.CodeMediaRejected,
				"Ce média a été rejeté par la modération.",
				nil,
			)
		}
		if mp.Visibility != variables.MediaVisibilityPending && mp.Visibility != variables.MediaVisibilityPublic {
			return numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				fmt.Sprintf("Le statut du média (%d) ne permet pas son rattachement à un article.", chk.mediaID),
				fmt.Errorf("media %d has visibility %d", chk.mediaID, mp.Visibility),
			)
		}

		ext := strings.ToLower(filepath.Ext(cleanPath))
		if chk.isMarkdown {
			if ext != ".md" {
				return numan_error.NewBadRequest(
					numan_error.CodeInvalidPayload,
					"Le champ markdown_media_id doit obligatoirement référencer un fichier Markdown (.md).",
					fmt.Errorf("media %d has extension %q instead of .md", chk.mediaID, ext),
				)
			}
		} else {
			if ext != ".avif" && ext != ".gif" && ext != ".webp" {
				return numan_error.NewBadRequest(
					numan_error.CodeInvalidPayload,
					fmt.Sprintf("Le champ %s doit référencer un asset visuel (.avif, .gif ou .webp).", chk.roleLabel),
					fmt.Errorf("media %d has extension %q instead of visual asset", chk.mediaID, ext),
				)
			}
		}
	}

	// Étape 2 : Activation en Public (1) des médias validés (idempotent si déjà Public)
	nowMs := domain.NowMillis()
	activatedOrder := make([]int64, 0, len(resolvedPayloads))
	seenActivated := make(map[int64]struct{}, len(resolvedPayloads))
	for _, chk := range checks {
		if _, done := seenActivated[chk.mediaID]; done {
			continue
		}
		seenActivated[chk.mediaID] = struct{}{}
		activatedOrder = append(activatedOrder, chk.mediaID)
	}

	for _, mediaID := range activatedOrder {
		mp := resolvedPayloads[mediaID]
		if mp.Visibility == variables.MediaVisibilityPublic {
			continue
		}
		mp.Visibility = variables.MediaVisibilityPublic
		mp.UpdatedAt = nowMs

		if errVal := mp.Validate(); errVal != nil {
			return errVal
		}
		if errCache := object_cache_service.SetMediaInObjectCache(ctx, mp); errCache != nil {
			numan_log.Error(ctx).Err(errCache).Int64("media_id", mediaID).Msg("Échec de la mise à jour L1 lors de l'activation d'un asset d'article")
			return errCache
		}
		if errQueue := redis.EnqueueDB(ctx, mediaID, mp.OwnerID, redis.EntityMedia, redis.ActionUpdate, mp, redis.TargetAll); errQueue != nil {
			numan_log.Error(ctx).Err(errQueue).Int64("media_id", mediaID).Msg("Échec d'EnqueueDB lors de l'activation d'un asset d'article")
			return numan_error.NewInternal()
		}
	}

	return nil
}

// DeactivateAdminMediaBatch désactive (Soft-Delete : Visibility = -1) une liste d'assets d'articles (`articles/`)
// uniquement s'ils ne sont référencés par aucun autre article actif (`visibility >= 0`) en cache L1 ou en PostgreSQL L3.
// Ne supprime jamais physiquement les fichiers S3 de manière synchrone (le Garbage Collector différé s'en charge après 48h).
func DeactivateAdminMediaBatch(ctx context.Context, candidateMediaIDs []int64, excludeArticleID int64) ([]int64, error) {
	if len(candidateMediaIDs) == 0 {
		return []int64{}, nil
	}

	seen := make(map[int64]struct{}, len(candidateMediaIDs))
	uniqueIDs := make([]int64, 0, len(candidateMediaIDs))
	for _, id := range candidateMediaIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			uniqueIDs = append(uniqueIDs, id)
		}
	}
	if len(uniqueIDs) == 0 {
		return []int64{}, nil
	}

	// 1. Vérification des références actives en cache L1 (`articles:index:data`)
	referencedInL1 := findArticleMediaReferencedInL1(ctx, uniqueIDs, excludeArticleID)

	candidatesAfterL1 := make([]int64, 0, len(uniqueIDs))
	for _, id := range uniqueIDs {
		if _, ref := referencedInL1[id]; !ref {
			candidatesAfterL1 = append(candidatesAfterL1, id)
		}
	}
	if len(candidatesAfterL1) == 0 {
		return []int64{}, nil
	}

	// 2. Vérification transactionnelle des références actives en PostgreSQL L3 (si initialisé)
	eligibleIDs := candidatesAfterL1
	if infraPostgres.PostgresDB != nil {
		unreferencedL3, errPg := postgres.FuncFilterUnreferencedArticleMedia(ctx, candidatesAfterL1, excludeArticleID)
		if errPg == nil {
			eligibleIDs = unreferencedL3
		}
	}

	nowMs := domain.NowMillis()
	deactivated := make([]int64, 0, len(eligibleIDs))

	for _, mediaID := range eligibleIDs {
		mp, errCascade := getMediaCascade(ctx, mediaID)
		if errCascade != nil {
			continue
		}

		cleanPath := strings.TrimPrefix(strings.TrimSpace(mp.StoragePath), "/")
		if !strings.HasPrefix(cleanPath, variables.ArticleMediaStoragePrefix) {
			continue
		}

		mp.Visibility = variables.MediaVisibilitySoftDeleted
		mp.UpdatedAt = nowMs

		_ = object_cache_service.SetMediaInObjectCache(ctx, mp)
		if errQueue := redis.EnqueueDB(ctx, mediaID, mp.OwnerID, redis.EntityMedia, redis.ActionUpdate, mp, redis.TargetAll); errQueue != nil {
			numan_log.Error(ctx).Err(errQueue).Int64("media_id", mediaID).Msg("Échec du Write-Behind lors de la désactivation d'un asset d'article")
			return deactivated, numan_error.NewInternal()
		}
		deactivated = append(deactivated, mediaID)
	}

	return deactivated, nil
}

// findArticleMediaReferencedInL1 inspecte le HASH L1 `articles:index:data` pour identifier les médias encore utilisés
// par au moins un autre article actif (`Visibility >= 0` et `ID != excludeArticleID`).
func findArticleMediaReferencedInL1(ctx context.Context, candidateIDs []int64, excludeArticleID int64) map[int64]struct{} {
	referenced := make(map[int64]struct{})
	if len(candidateIDs) == 0 || infraRedis.Rdb == nil {
		return referenced
	}

	candidateSet := make(map[int64]struct{}, len(candidateIDs))
	for _, id := range candidateIDs {
		candidateSet[id] = struct{}{}
	}

	allRaw, err := infraRedis.Rdb.HGetAll(ctx, variables.RedisKeyArticlesIndexData).Result()
	if err != nil || len(allRaw) == 0 {
		return referenced
	}

	for field, rawStr := range allRaw {
		artID, _ := strconv.ParseInt(field, 10, 64)
		if excludeArticleID > 0 && artID == excludeArticleID {
			continue
		}
		var art article_models.ArticlePayload
		if errUn := msgpack.Unmarshal([]byte(rawStr), &art); errUn != nil {
			continue
		}
		if excludeArticleID > 0 && art.ID == excludeArticleID {
			continue
		}
		if art.Visibility < 0 {
			continue
		}
		for _, refID := range art.ReferencedMediaIDs() {
			if _, isCandidate := candidateSet[refID]; isCandidate {
				referenced[refID] = struct{}{}
			}
		}
	}

	return referenced
}
