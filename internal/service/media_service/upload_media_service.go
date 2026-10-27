package media_service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/minio"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/disintegration/imaging"
	"github.com/gen2brain/avif"
	"github.com/google/uuid"
	miniogo "github.com/minio/minio-go/v7"
)

// ############################################################################
// # SERVICE : UPLOAD ET TRAITEMENT DES MÉDIAS (AVIF & MINIO)
// ############################################################################

// UploadMediaWithStatusAndIAScore traite l'image, l'uploade sur S3/MinIO et l'enregistre avec son statut et son score IA NSFWJS [0.0, 1.0].
func UploadMediaWithStatusAndIAScore(fileStream io.ReadSeeker, ownerID int64, mediaID int64, initialVisibility int, iaScore float64) error {
	if iaScore < 0.0 || iaScore > 1.0 || math.IsNaN(iaScore) || math.IsInf(iaScore, 0) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le score IA doit être compris entre 0.0 et 1.0.", nil)
	}

	// ── ÉTAPE 1 : ANALYSE ET VALIDATION DE L'IMAGE ──────────────────────────

	imageConfig, _, errConfig := image.DecodeConfig(fileStream)
	if errConfig != nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le fichier fourni n'est pas une image valide ou est corrompu.", errConfig)
	}

	if imageConfig.Width*imageConfig.Height > variables.MediaMaxPixels {
		return numan_error.NewBadRequest(numan_error.CodePayloadTooLarge, "La résolution de l'image dépasse la limite maximale autorisée.", nil)
	}

	// Remise à zéro du curseur de lecture après l'analyse
	if _, errSeek := fileStream.Seek(0, io.SeekStart); errSeek != nil {
		numan_log.Error(context.Background()).Err(errSeek).Msg("Erreur lors du reset du curseur de lecture du fichier uploadé")
		return numan_error.NewInternal()
	}

	// ── ÉTAPE 2 : DÉCODAGE, RESIZING ET ENCODAGE AVIF (CPU HEAVY) ───────────

	decodedImage, _, errDecode := image.Decode(fileStream)
	if errDecode != nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Erreur lors du décodage structurel de l'image.", errDecode)
	}

	// Réduction homothétique si l'image est trop large
	if bounds := decodedImage.Bounds(); bounds.Dx() > variables.MediaMaxWidth {
		decodedImage = imaging.Resize(decodedImage, variables.MediaMaxWidth, 0, imaging.Lanczos)
	}

	var avifBuffer bytes.Buffer
	encodingOptions := avif.Options{
		Quality: variables.MediaAvifQuality,
		Speed:   variables.MediaAvifSpeed,
	}

	if errEncode := avif.Encode(&avifBuffer, decodedImage, encodingOptions); errEncode != nil {
		numan_log.Error(context.Background()).Err(errEncode).Msg("Erreur interne lors de l'encodage AVIF de l'image")
		return numan_error.NewInternal()
	}

	// ── ÉTAPE 3 : UPLOAD SÉCURISÉ VERS OBJECT STORAGE (MINIO/S3) ────────────

	uniqueFileName := fmt.Sprintf("%s.avif", uuid.New().String())
	storagePath := fmt.Sprintf("users/%d/media/%s", ownerID, uniqueFileName)

	bucketName := os.Getenv("MINIO_BUCKET_NAME")
	if bucketName == "" {
		bucketName = "numan-bucket"
	}

	_, errUpload := minio.MinioClient.PutObject(
		context.Background(),
		bucketName,
		storagePath,
		&avifBuffer,
		int64(avifBuffer.Len()),
		miniogo.PutObjectOptions{
			ContentType: "image/avif",
		},
	)

	if errUpload != nil {
		numan_log.Error(context.Background()).Err(errUpload).Str("path", storagePath).Msg("Échec de l'upload du fichier vers l'Object Storage (MinIO/S3)")
		return numan_error.NewInternal()
	}

	// ── ÉTAPE 4 : CRÉATION DU MODÈLE MÉTIER ET MISE EN CACHE L1 ─────────────

	currentTime := time.Now().UTC()
	mediaPayload := media_models.MediaPayload{
		ID:          mediaID,
		OwnerID:     ownerID,
		StoragePath: storagePath,
		Visibility:  initialVisibility, // 0=Pending, 1=Public, 2=Quarantaine
		CreatedAt:   domain.TimeToMillis(currentTime),
		UpdatedAt:   domain.TimeToMillis(currentTime),
	}

	ctx := context.Background()

	if errCache := object_cache_service.SetMediaInObjectCache(ctx, mediaPayload); errCache != nil {
		numan_log.Error(context.Background()).Err(errCache).Int64("media_id", mediaID).Msg("Échec de la mise en cache L1 du Média")
	}

	// ── ÉTAPE 5 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ET ROLLBACK ─────────

	errQueue := redis.EnqueueDB(ctx, mediaID, ownerID, redis.EntityMedia, redis.ActionCreate, mediaPayload, redis.TargetAll)

	if errQueue != nil {
		numan_log.Error(context.Background()).Err(errQueue).Int64("media_id", mediaID).Msg("Impossible d'enqueue le Média, lancement du Rollback S3...")
		// ROLLBACK : On supprime le fichier orphelin sur le S3 si la BDD n'a pas pu être notifiée
		_ = minio.MinioClient.RemoveObject(context.Background(), bucketName, storagePath, miniogo.RemoveObjectOptions{})

		return numan_error.NewInternal()
	}

	numan_log.Info(context.Background()).
		Int64("media_id", mediaID).
		Int("visibility", initialVisibility).
		Int64("owner_id", ownerID).
		Msg("Nouveau média traité et uploadé avec succès")

	return nil
}
