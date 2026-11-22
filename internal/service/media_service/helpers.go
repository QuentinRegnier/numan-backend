package media_service

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/minio"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/security"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	miniogo "github.com/minio/minio-go/v7"
	"github.com/vmihailenco/msgpack/v5"
	"go.mongodb.org/mongo-driver/bson"
)

// ############################################################################
// # UTILITAIRES : BUCKET ROUTING & URLS PUBLIQUES S3 (ARTICLES / MAPS)
// ############################################################################

// resolveBucketForStoragePath détermine le bucket S3/MinIO cible selon le préfixe du chemin de stockage.
// Les assets publics d'articles (`articles/`) sont stockés dans `MINIO_PUBLIC_BUCKET_NAME` (défaut: `numan-public`).
// Tous les autres médias utilisateurs restent dans `MINIO_BUCKET_NAME` (défaut: `numan-bucket`).
func resolveBucketForStoragePath(storagePath string) string {
	cleanPath := strings.TrimPrefix(strings.TrimSpace(storagePath), "/")
	if strings.HasPrefix(cleanPath, variables.ArticleMediaStoragePrefix) {
		pubBucket := strings.TrimSpace(os.Getenv("MINIO_PUBLIC_BUCKET_NAME"))
		if pubBucket == "" {
			return variables.DefaultPublicBucketName
		}
		return pubBucket
	}

	privBucket := strings.TrimSpace(os.Getenv("MINIO_BUCKET_NAME"))
	if privBucket == "" {
		return variables.DefaultPrivateBucketName
	}
	return privBucket
}

// GeneratePublicMediaURL génère une URL publique directe (sans signature HMAC ni filigrane)
// à partir de `PUBLIC_MEDIA_BASE_URL` et d'un `storagePath` relatif sûr.
func GeneratePublicMediaURL(storagePath string) string {
	trimmed := strings.TrimSpace(storagePath)
	if trimmed == "" {
		return ""
	}
	// Rejet strict des URLs externes, schémas injectés, traversées de répertoires ou caractères de contrôle
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "://") ||
		strings.HasPrefix(lower, "//") ||
		strings.Contains(trimmed, "..") ||
		strings.Contains(trimmed, "\\") ||
		strings.ContainsAny(trimmed, " \t\r\n\x00?#") {
		return ""
	}

	cleanPath := strings.TrimPrefix(trimmed, "/")
	if cleanPath == "" {
		return ""
	}

	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_MEDIA_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = "/public"
	}

	return fmt.Sprintf("%s/%s", baseURL, cleanPath)
}

// ResolvePublicMediaMapCascade résout en lot (MGET L1 puis cascade L1 -> L2 -> L3 pour les misses)
// une liste d'identifiants de médias d'articles publics (`Visibility == MediaVisibilityPublic` et préfixe `articles/`).
func ResolvePublicMediaMapCascade(ctx context.Context, mediaIDs []int64) map[int64]article_models.ArticleAssetView {
	result := make(map[int64]article_models.ArticleAssetView)
	if len(mediaIDs) == 0 {
		return result
	}

	// Déduplication des IDs strictement positifs
	seen := make(map[int64]struct{}, len(mediaIDs))
	uniqueIDs := make([]int64, 0, len(mediaIDs))
	for _, id := range mediaIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			uniqueIDs = append(uniqueIDs, id)
		}
	}
	if len(uniqueIDs) == 0 {
		return result
	}

	isEligibleArticleAsset := func(mp media_models.MediaPayload) (article_models.ArticleAssetView, bool) {
		cleanPath := strings.TrimPrefix(strings.TrimSpace(mp.StoragePath), "/")
		if mp.ID <= 0 || mp.Visibility != variables.MediaVisibilityPublic {
			return article_models.ArticleAssetView{}, false
		}
		if !strings.HasPrefix(cleanPath, variables.ArticleMediaStoragePrefix) {
			return article_models.ArticleAssetView{}, false
		}
		pubURL := GeneratePublicMediaURL(cleanPath)
		if pubURL == "" {
			return article_models.ArticleAssetView{}, false
		}
		return article_models.ArticleAssetView{
			MediaID:     mp.ID,
			StoragePath: cleanPath,
			URL:         pubURL,
		}, true
	}

	// 1. Batch L1 MGET
	missedIDs := make([]int64, 0)
	if len(uniqueIDs) > 1 && redis.IsReady() && redis.Media != nil {
		if res, errMany := redis.Media.GetMany(ctx, uniqueIDs); errMany == nil && res != nil {
			for _, id := range uniqueIDs {
				raw, found := res.Found[id]
				if !found {
					missedIDs = append(missedIDs, id)
					continue
				}
				var mp media_models.MediaPayload
				if errUn := msgpack.Unmarshal(raw, &mp); errUn != nil {
					missedIDs = append(missedIDs, id)
					continue
				}
				if view, ok := isEligibleArticleAsset(mp); ok {
					result[id] = view
				}
			}
		} else {
			missedIDs = uniqueIDs
		}
	} else {
		missedIDs = uniqueIDs
	}

	// 2. Fallback cascade L1 -> L2 -> L3 pour les misses uniquement
	for _, id := range missedIDs {
		mp, errCascade := getMediaCascade(ctx, id)
		if errCascade != nil {
			continue
		}
		if view, ok := isEligibleArticleAsset(mp); ok {
			result[id] = view
		}
	}

	return result
}

// ############################################################################
// # UTILITAIRES : SÉCURITÉ ET FILIGRANAGE DES URLS (WATERMARK)
// ############################################################################

// generateWatermarkedURL crée l'URL finale pointant vers le micro-service de tatouage.
// Elle est signée cryptographiquement avec HMAC pour empêcher la falsification des paramètres.
func generateWatermarkedURL(mediaStoragePath string, authorID int64, contextID int64, readerID int64) string {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("WATERMARK_API_URL")), "/")
	secretKey := os.Getenv("WATERMARK_SECRET_KEY")
	currentTimestamp := domain.TimeToSeconds(domain.NowTime())

	// Construction stricte de la chaîne de paramètres à signer
	signaturePayload := fmt.Sprintf("key=%s&author=%d&post=%d&reader=%d&ts=%d", mediaStoragePath, authorID, contextID, readerID, currentTimestamp)

	// Calcul de la signature HMAC-SHA256
	hmacSignature := security.GenerateHMAC(signaturePayload, secretKey)

	// URL finale formatée pour l'API
	return fmt.Sprintf("%s/process?%s&sig=%s", baseURL, signaturePayload, hmacSignature)
}

// GenerateMediaViewCascade récupère le média (L1->L2->L3) et génère le DTO final (URL signée).
// Bloque strictement le chargement si le média est en quarantaine (Visibility = 2) ou non public.
func GenerateMediaViewCascade(ctx context.Context, targetMediaID, authorID, contextID, readerID int64) (media_models.MediaView, error) {
	mediaPayload, errCascade := getMediaCascade(ctx, targetMediaID)
	if errCascade != nil {
		return media_models.MediaView{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Média introuvable ou supprimé.", errCascade)
	}

	if mediaPayload.Visibility == variables.MediaVisibilityQuarantine {
		return media_models.MediaView{}, numan_error.NewForbidden(
			numan_error.CodeMediaUnderReview,
			"Ce média est en attente de validation par un modérateur et ne peut pas être chargé.",
			nil,
		)
	}
	if mediaPayload.Visibility == variables.MediaVisibilityRejected {
		return media_models.MediaView{}, numan_error.NewForbidden(
			numan_error.CodeMediaRejected,
			"Ce média a été retiré par la modération.",
			nil,
		)
	}
	if mediaPayload.Visibility != variables.MediaVisibilityPublic {
		return media_models.MediaView{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Média introuvable ou supprimé.", nil)
	}

	signedURL := generateWatermarkedURL(mediaPayload.StoragePath, authorID, contextID, readerID)

	return media_models.MediaView{
		MediaID: targetMediaID,
		URL:     signedURL,
	}, nil
}

// FormatMediaViewsCascade prend une liste d'IDs, résout la cascade d'auto-guérison en privilégiant
// un MGET groupé en mémoire L1 pour éviter le problème N+1, et retourne un tableau de MediaViews sécurisées.
func FormatMediaViewsCascade(ctx context.Context, mediaIDs []int64, authorID, contextID, readerID int64) []media_models.MediaView {
	if len(mediaIDs) == 0 {
		return make([]media_models.MediaView, 0)
	}

	hydratedViews := make([]media_models.MediaView, 0, len(mediaIDs))

	// Optimisation Batch L1 : un seul appel MGET Redis lorsque plusieurs médias sont demandés
	var batchL1 map[int64]media_models.MediaPayload
	if len(mediaIDs) > 1 && redis.IsReady() && redis.Media != nil {
		if res, errMany := redis.Media.GetMany(ctx, mediaIDs); errMany == nil && res != nil {
			batchL1 = make(map[int64]media_models.MediaPayload, len(res.Found))
			for id, raw := range res.Found {
				var mp media_models.MediaPayload
				if errUn := msgpack.Unmarshal(raw, &mp); errUn == nil {
					batchL1[id] = mp
				}
			}
		}
	}

	for _, id := range mediaIDs {
		if mp, ok := batchL1[id]; ok {
			if mp.Visibility == variables.MediaVisibilityPublic {
				hydratedViews = append(hydratedViews, media_models.MediaView{
					MediaID: id,
					URL:     generateWatermarkedURL(mp.StoragePath, authorID, contextID, readerID),
				})
			}
			continue
		}
		// Fallback cascade individuel L1 -> L2 -> L3 en cas de miss L1
		if view, err := GenerateMediaViewCascade(ctx, id, authorID, contextID, readerID); err == nil {
			hydratedViews = append(hydratedViews, view)
		}
	}

	return hydratedViews
}

// ############################################################################
// # UTILITAIRES : GESTION DES MÉDIAS (CASCADE ET PHYSIQUE)
// ############################################################################

// getMediaCascade récupère les informations d'un média avec une stratégie L1 -> L2 -> L3
// et réhydrate automatiquement les caches manquants.
func getMediaCascade(ctx context.Context, mediaID int64) (media_models.MediaPayload, error) {

	// ── TENTATIVE L1 (RAM OBJECT CACHE) ───────────────────────────────────────
	if mediaPayload, errCache := object_cache_service.GetMediaFromObjectCache(ctx, mediaID); errCache == nil {
		return mediaPayload, nil
	}

	// ── TENTATIVE L2 (MONGODB WARM STORAGE) ───────────────────────────────────
	mediaFromMongo, errMongo := mongo.MongoGetMediaByID(ctx, mediaID)
	if errMongo == nil && mediaFromMongo.ID != 0 {
		_ = object_cache_service.SetMediaInObjectCache(ctx, mediaFromMongo)
		return mediaFromMongo, nil
	}

	// ── TENTATIVE L3 (POSTGRESQL COLD STORAGE) ────────────────────────────────
	mediaFromPostgres, errPg := postgres.FuncGetMedia(ctx, mediaID)
	if errPg != nil {
		numan_log.Error(ctx).Err(errPg).Int64("media_id", mediaID).Msg("Erreur L3 lors de la récupération du média en cascade")
		return media_models.MediaPayload{}, numan_error.NewInternal()
	}

	if mediaFromPostgres.ID != 0 {
		// AUTO-GUÉRISON L1 (Synchrone en RAM)
		_ = object_cache_service.SetMediaInObjectCache(ctx, mediaFromPostgres)

		// AUTO-GUÉRISON L2 (Asynchrone vers Mongo via Worker)
		go func(m media_models.MediaPayload) {
			bgCtx := context.Background()
			// La clé de partition est OwnerID pour grouper les médias d'un même utilisateur
			_ = redis.EnqueueDB(bgCtx, m.ID, m.OwnerID, redis.EntityMedia, redis.ActionUpdate, m, redis.TargetMongo)
		}(mediaFromPostgres)

		return mediaFromPostgres, nil
	}

	return media_models.MediaPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Média introuvable ou expiré.", nil)
}

// RemovePhysicalMedia détruit physiquement le fichier sur le stockage S3/MinIO.
// Utilisé par les suppressions unitaires et le Garbage Collector asynchrone.
// Protège strictement les préfixes statiques opérateur (`maps/` et `static/`).
func RemovePhysicalMedia(ctx context.Context, storagePath string) error {
	cleanPath := strings.TrimPrefix(strings.TrimSpace(storagePath), "/")
	if cleanPath == "" {
		return nil
	}
	if strings.HasPrefix(cleanPath, variables.MapsStoragePrefix) || strings.HasPrefix(cleanPath, variables.StaticStoragePrefix) {
		return nil
	}
	if minio.MinioClient == nil {
		return numan_error.NewInternal()
	}

	bucketName := resolveBucketForStoragePath(cleanPath)

	err := minio.MinioClient.RemoveObject(ctx, bucketName, cleanPath, miniogo.RemoveObjectOptions{})
	if err != nil {
		numan_log.Error(ctx).Err(err).Str("bucket", bucketName).Str("path", cleanPath).Msg("Échec de la suppression physique du fichier sur MinIO")
		return numan_error.NewInternal()
	}

	return nil
}

// DecodeMediaVisibilityJSON désérialise de manière rétrocompatible une visibilité JSON (booléen legacy ou entier).
func DecodeMediaVisibilityJSON(data []byte) (int, error) {
	str := string(data)
	if str == "true" {
		return variables.MediaVisibilityPublic, nil
	}
	if str == "false" {
		return variables.MediaVisibilityPending, nil
	}
	val, err := strconv.Atoi(str)
	if err != nil {
		return 0, numan_error.NewAppError(http.StatusBadRequest, numan_error.CodeInvalidPayload, "Valeur JSON invalide pour MediaVisibility", err)
	}
	return val, nil
}

// DecodeMediaVisibilityBSON désérialise de manière rétrocompatible une valeur BSON
// (booléen legacy ou entier) via l'API moderne bson.RawValue.
func DecodeMediaVisibilityBSON(raw bson.RawValue) (int, error) {
	switch raw.Type {
	case bson.TypeBoolean:
		if raw.Boolean() {
			return variables.MediaVisibilityPublic, nil
		}
		return variables.MediaVisibilityPending, nil

	case bson.TypeInt32:
		if len(raw.Value) != 4 {
			return 0, numan_error.NewAppError(
				http.StatusBadRequest,
				numan_error.CodeInvalidPayload,
				"Valeur BSON Int32 invalide pour MediaVisibility",
				nil,
			)
		}
		return int(int32(binary.LittleEndian.Uint32(raw.Value))), nil

	case bson.TypeInt64:
		if len(raw.Value) != 8 {
			return 0, numan_error.NewAppError(
				http.StatusBadRequest,
				numan_error.CodeInvalidPayload,
				"Valeur BSON Int64 invalide pour MediaVisibility",
				nil,
			)
		}
		return int(int64(binary.LittleEndian.Uint64(raw.Value))), nil

	case bson.TypeDouble:
		if len(raw.Value) != 8 {
			return 0, numan_error.NewAppError(
				http.StatusBadRequest,
				numan_error.CodeInvalidPayload,
				"Valeur BSON Double invalide pour MediaVisibility",
				nil,
			)
		}
		value := math.Float64frombits(binary.LittleEndian.Uint64(raw.Value))
		return int(value), nil

	default:
		return 0, numan_error.NewAppError(
			http.StatusBadRequest,
			numan_error.CodeInvalidPayload,
			fmt.Sprintf(
				"Type BSON non supporté %v pour MediaVisibility",
				raw.Type,
			),
			nil,
		)
	}
}
