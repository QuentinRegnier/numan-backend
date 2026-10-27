package media_service

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

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
	"go.mongodb.org/mongo-driver/bson"
)

// ############################################################################
// # UTILITAIRES : SÉCURITÉ ET FILIGRANAGE DES URLS (WATERMARK)
// ############################################################################

// generateWatermarkedURL crée l'URL finale pointant vers le micro-service de tatouage.
// Elle est signée cryptographiquement avec HMAC pour empêcher la falsification des paramètres.
func generateWatermarkedURL(mediaStoragePath string, authorID int64, contextID int64, readerID int64) string {
	baseURL := os.Getenv("WATERMARK_API_URL")
	secretKey := os.Getenv("WATERMARK_SECRET_KEY")
	currentTimestamp := time.Now().Unix()

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

// FormatMediaViewsCascade prend une liste d'IDs, résout la cascade d'auto-guérison pour chacun,
// et retourne un tableau de MediaViews sécurisées.
func FormatMediaViewsCascade(ctx context.Context, mediaIDs []int64, authorID, contextID, readerID int64) []media_models.MediaView {
	var hydratedViews []media_models.MediaView

	for _, id := range mediaIDs {
		// En cas d'erreur (ex: média supprimé modéré), on l'ignore silencieusement
		// pour ne pas faire crasher l'affichage de l'entier du Post/Conversation.
		if view, err := GenerateMediaViewCascade(ctx, id, authorID, contextID, readerID); err == nil {
			hydratedViews = append(hydratedViews, view)
		}
	}

	// Prévention stricte du retour `null` en JSON
	if hydratedViews == nil {
		hydratedViews = make([]media_models.MediaView, 0)
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
func RemovePhysicalMedia(ctx context.Context, storagePath string) error {
	if storagePath == "" {
		return nil
	}

	bucketName := os.Getenv("MINIO_BUCKET_NAME")
	if bucketName == "" {
		bucketName = "numan-bucket" // Fallback par défaut de l'infrastructure
	}

	err := minio.MinioClient.RemoveObject(ctx, bucketName, storagePath, miniogo.RemoveObjectOptions{})
	if err != nil {
		numan_log.Error(ctx).Err(err).Str("path", storagePath).Msg("Échec de la suppression physique du fichier sur MinIO")
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
