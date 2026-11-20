package media_service

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

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
	_ "golang.org/x/image/webp"
)

// ############################################################################
// # SERVICE : UPLOAD ET TRAITEMENT DES MÉDIAS (AVIF & MINIO)
// ############################################################################

// UploadMediaWithStatusAndIAScore traite l'image, l'uploade sur S3/MinIO et l'enregistre avec son statut et son score IA NSFWJS [0.0, 1.0].
func UploadMediaWithStatusAndIAScore(fileStream io.ReadSeeker, ownerID int64, mediaID int64, initialVisibility int, iaScore float64) error {
	if ownerID <= 0 {
		return numan_error.NewUnauthorized(numan_error.CodeUserIsNotIdentified, "Utilisateur non identifié.", nil)
	}
	if mediaID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du média doit être strictement positif.", nil)
	}
	if fileStream == nil {
		return numan_error.NewBadRequest(numan_error.CodeMissingFile, "Le flux du fichier média est manquant.", nil)
	}
	switch initialVisibility {
	case variables.MediaVisibilityRejected,
		variables.MediaVisibilitySoftDeleted,
		variables.MediaVisibilityPending,
		variables.MediaVisibilityPublic,
		variables.MediaVisibilityQuarantine:
	default:
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le statut de visibilité initial du média est invalide.", nil)
	}
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

	currentTime := domain.NowTime()
	mediaPayload := media_models.MediaPayload{
		ID:          mediaID,
		OwnerID:     ownerID,
		StoragePath: storagePath,
		Visibility:  initialVisibility, // 0=Pending, 1=Public, 2=Quarantaine
		IAScore:     iaScore,
		CreatedAt:   domain.TimeToMillis(currentTime),
		UpdatedAt:   domain.TimeToMillis(currentTime),
	}

	ctx := context.Background()
	if err := mediaPayload.Validate(); err != nil {
		_ = minio.MinioClient.RemoveObject(ctx, bucketName, storagePath, miniogo.RemoveObjectOptions{})
		return err
	}

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

// ############################################################################
// # SERVICE : UPLOAD D'ASSETS D'ARTICLES PUBLICS (ADMIN)
// ############################################################################

// UploadAdminArticleAsset valide, traite et uploade un asset d'article (image statique -> AVIF, GIF animé -> GIF,
// WebP animé -> WebP, document Markdown -> .md) dans le bucket S3 public (`numan-public`) sous `articles/<uuid>.<ext>`.
func UploadAdminArticleAsset(
	ctx context.Context,
	fileStream io.ReadSeeker,
	adminID int64,
	mediaID int64,
	assetKind string,
	qualityTier string,
) (media_models.UploadAdminArticleAssetOutput, error) {
	if adminID <= 0 {
		return media_models.UploadAdminArticleAssetOutput{}, numan_error.NewUnauthorized(
			numan_error.CodeUserIsNotIdentified,
			"Administrateur non identifié.",
			nil,
		)
	}
	if mediaID <= 0 {
		return media_models.UploadAdminArticleAssetOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'identifiant du média doit être strictement positif.",
			nil,
		)
	}
	if fileStream == nil {
		return media_models.UploadAdminArticleAssetOutput{}, numan_error.NewBadRequest(
			numan_error.CodeMissingFile,
			"Le flux du fichier est manquant.",
			nil,
		)
	}
	if minio.MinioClient == nil {
		return media_models.UploadAdminArticleAssetOutput{}, numan_error.NewInternal()
	}

	kindNorm := strings.ToLower(strings.TrimSpace(assetKind))
	switch kindNorm {
	case "", variables.ArticleAssetKindImage:
		kindNorm = variables.ArticleAssetKindImage
	case variables.ArticleAssetKindMarkdown, "md":
		kindNorm = variables.ArticleAssetKindMarkdown
	default:
		return media_models.UploadAdminArticleAssetOutput{}, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le type d'asset (kind) est invalide. Valeurs supportées : 'image', 'markdown'.",
			fmt.Errorf("unsupported article asset kind: %q", assetKind),
		)
	}

	maxDim, errTier := resolveArticleImageQualityMaxDim(qualityTier)
	if errTier != nil {
		return media_models.UploadAdminArticleAssetOutput{}, errTier
	}

	var (
		payloadBytes []byte
		ext          string
		contentType  string
		errProcess   error
	)

	if kindNorm == variables.ArticleAssetKindMarkdown {
		payloadBytes, errProcess = processAndValidateMarkdownAsset(fileStream)
		ext = "md"
		contentType = "text/markdown; charset=utf-8"
	} else {
		payloadBytes, ext, contentType, errProcess = processAndValidateVisualArticleAsset(fileStream, maxDim)
	}
	if errProcess != nil {
		return media_models.UploadAdminArticleAssetOutput{}, errProcess
	}

	storagePath := fmt.Sprintf("%s%s.%s", variables.ArticleMediaStoragePrefix, uuid.New().String(), ext)
	bucketName := resolveBucketForStoragePath(storagePath)

	_, errUpload := minio.MinioClient.PutObject(
		ctx,
		bucketName,
		storagePath,
		bytes.NewReader(payloadBytes),
		int64(len(payloadBytes)),
		miniogo.PutObjectOptions{
			ContentType: contentType,
		},
	)
	if errUpload != nil {
		numan_log.Error(ctx).Err(errUpload).Str("bucket", bucketName).Str("path", storagePath).Msg("Échec de l'upload de l'asset d'article vers MinIO")
		return media_models.UploadAdminArticleAssetOutput{}, numan_error.NewInternal()
	}

	nowMs := domain.NowMillis()
	mediaPayload := media_models.MediaPayload{
		ID:          mediaID,
		OwnerID:     adminID,
		StoragePath: storagePath,
		Visibility:  variables.MediaVisibilityPending, // Activé en Public (1) lors de la création/mise à jour de l'article
		IAScore:     0.0,
		CreatedAt:   nowMs,
		UpdatedAt:   nowMs,
	}

	if errVal := mediaPayload.Validate(); errVal != nil {
		_ = minio.MinioClient.RemoveObject(ctx, bucketName, storagePath, miniogo.RemoveObjectOptions{})
		return media_models.UploadAdminArticleAssetOutput{}, errVal
	}

	if errCache := object_cache_service.SetMediaInObjectCache(ctx, mediaPayload); errCache != nil {
		numan_log.Error(ctx).Err(errCache).Int64("media_id", mediaID).Msg("Échec de la mise en cache L1 de l'asset d'article, rollback S3...")
		_ = minio.MinioClient.RemoveObject(ctx, bucketName, storagePath, miniogo.RemoveObjectOptions{})
		return media_models.UploadAdminArticleAssetOutput{}, numan_error.NewInternal()
	}

	if errQueue := redis.EnqueueDB(ctx, mediaID, adminID, redis.EntityMedia, redis.ActionCreate, mediaPayload, redis.TargetAll); errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("media_id", mediaID).Msg("Échec d'EnqueueDB pour l'asset d'article, rollback L1 + S3...")
		_ = object_cache_service.DeleteMediaFromObjectCache(ctx, mediaID)
		_ = minio.MinioClient.RemoveObject(ctx, bucketName, storagePath, miniogo.RemoveObjectOptions{})
		return media_models.UploadAdminArticleAssetOutput{}, numan_error.NewInternal()
	}

	publicURL := GeneratePublicMediaURL(storagePath)

	numan_log.Info(ctx).
		Int64("media_id", mediaID).
		Int64("user_id", adminID).
		Str("path", storagePath).
		Str("content_type", contentType).
		Msg("Asset d'article uploadé avec succès dans le bucket public")

	return media_models.UploadAdminArticleAssetOutput{
		MediaID:     mediaID,
		StoragePath: storagePath,
		URL:         publicURL,
		ContentType: contentType,
	}, nil
}

// resolveArticleImageQualityMaxDim résout le palier de qualité ('standard' / '1024' ou 'high' / '2048').
func resolveArticleImageQualityMaxDim(qualityTier string) (int, error) {
	trimmed := strings.ToLower(strings.TrimSpace(qualityTier))
	switch trimmed {
	case "", variables.ArticleQualityTierStandard, strconv.Itoa(variables.ArticleImageMaxDimStandard):
		return variables.ArticleImageMaxDimStandard, nil
	case variables.ArticleQualityTierHigh, strconv.Itoa(variables.ArticleImageMaxDimHigh):
		return variables.ArticleImageMaxDimHigh, nil
	default:
		return 0, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le palier de qualité (quality) est invalide. Valeurs supportées : 'standard' (1024px) ou 'high' (2048px).",
			fmt.Errorf("invalid quality tier: %q", qualityTier),
		)
	}
}

// readBoundedStream lit au maximum maxBytes du flux et retourne CodePayloadTooLarge si la taille dépasse maxBytes.
func readBoundedStream(r io.ReadSeeker, maxBytes int64) ([]byte, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, numan_error.NewInternal()
	}
	limited := io.LimitReader(r, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeFileReadError,
			"Impossible de lire le flux du fichier uploadé.",
			err,
		)
	}
	if int64(len(data)) > maxBytes {
		return nil, numan_error.NewBadRequest(
			numan_error.CodePayloadTooLarge,
			fmt.Sprintf("La taille du fichier dépasse la limite maximale autorisée (%d octets).", maxBytes),
			fmt.Errorf("upload size exceeds max %d bytes", maxBytes),
		)
	}
	if len(data) == 0 {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le fichier fourni est vide.",
			nil,
		)
	}
	return data, nil
}

// processAndValidateMarkdownAsset valide strictement un document Markdown UTF-8 (max 256 Ko, sans NUL ni exécutable/HTML déguisé).
func processAndValidateMarkdownAsset(r io.ReadSeeker) ([]byte, error) {
	data, err := readBoundedStream(r, variables.MaxArticleMarkdownBytes)
	if err != nil {
		return nil, err
	}

	// Rejet des signatures binaires connues (ELF, MZ/PE, Mach-O, ZIP, PDF, PNG, JPEG, GIF, RIFF)
	if hasBinaryMagicHeader(data) {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le fichier Markdown fourni est un fichier binaire déguisé.",
			nil,
		)
	}

	if bytes.IndexByte(data, 0x00) != -1 {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le document Markdown contient des octets NUL interdits.",
			nil,
		)
	}

	if !utf8.Valid(data) {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le document Markdown doit être encodé en UTF-8 valide.",
			nil,
		)
	}

	// Vérification de l'absence de caractères de contrôle binaires (hors \t, \n, \r)
	for _, b := range data {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
			return nil, numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				"Le document Markdown contient des caractères de contrôle binaires non autorisés.",
				fmt.Errorf("control byte 0x%02x in markdown", b),
			)
		}
	}

	text := strings.TrimSpace(strings.TrimPrefix(string(data), "\uFEFF"))
	if text == "" {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le document Markdown ne peut pas être vide.",
			nil,
		)
	}

	// Rejet des documents HTML / SVG / Script déguisés en Markdown
	lowerHead := strings.ToLower(text)
	for _, forbiddenPrefix := range []string{
		"<!doctype html",
		"<html",
		"<svg",
		"<?xml",
		"<script",
		"<iframe",
		"<object",
		"<embed",
	} {
		if strings.HasPrefix(lowerHead, forbiddenPrefix) {
			return nil, numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				"Les documents HTML, SVG ou XML autonomes ne sont pas autorisés comme fichiers Markdown.",
				fmt.Errorf("disguised markup prefix: %s", forbiddenPrefix),
			)
		}
	}
	if strings.Contains(lowerHead, "<script") ||
		strings.Contains(lowerHead, "javascript:") ||
		strings.Contains(lowerHead, "vbscript:") {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le document Markdown contient des balises ou instructions de script interdites.",
			nil,
		)
	}

	return data, nil
}

func hasBinaryMagicHeader(data []byte) bool {
	if len(data) >= 4 {
		// ELF, PNG, GIF, RIFF, PDF, ZIP, Mach-O
		if bytes.HasPrefix(data, []byte{0x7F, 'E', 'L', 'F'}) ||
			bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G'}) ||
			bytes.HasPrefix(data, []byte("GIF8")) ||
			bytes.HasPrefix(data, []byte("RIFF")) ||
			bytes.HasPrefix(data, []byte("%PDF")) ||
			bytes.HasPrefix(data, []byte{'P', 'K', 0x03, 0x04}) ||
			bytes.HasPrefix(data, []byte{0xFE, 0xED, 0xFA, 0xCE}) ||
			bytes.HasPrefix(data, []byte{0xFE, 0xED, 0xFA, 0xCF}) ||
			bytes.HasPrefix(data, []byte{0xCF, 0xFA, 0xED, 0xFE}) ||
			bytes.HasPrefix(data, []byte{0xCE, 0xFA, 0xED, 0xFE}) {
			return true
		}
	}
	if len(data) >= 3 && bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}) {
		return true
	}
	if len(data) >= 2 && bytes.HasPrefix(data, []byte{'M', 'Z'}) {
		return true
	}
	return false
}

// processAndValidateVisualArticleAsset inspecte les magic bytes, valide la structure, préserve les GIF/WebP animés
// ou redimensionne (sans upscaling ni rognage via imaging.Fit) et encode en AVIF les images statiques.
func processAndValidateVisualArticleAsset(r io.ReadSeeker, maxDim int) ([]byte, string, string, error) {
	// Lecture bornée à la limite maximale des images statiques (15 Mo)
	data, err := readBoundedStream(r, variables.MaxArticleStaticImageBytes)
	if err != nil {
		return nil, "", "", err
	}

	// 1. Détection GIF (`GIF87a` ou `GIF89a`)
	if len(data) >= 6 && (bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a"))) {
		if int64(len(data)) > variables.MaxArticleAnimatedGIFBytes {
			return nil, "", "", numan_error.NewBadRequest(
				numan_error.CodePayloadTooLarge,
				fmt.Sprintf("La taille du GIF dépasse la limite maximale autorisée (%d octets).", variables.MaxArticleAnimatedGIFBytes),
				nil,
			)
		}
		if errGIF := validateGIFStructure(data); errGIF != nil {
			return nil, "", "", errGIF
		}
		return data, "gif", "image/gif", nil
	}

	// 2. Détection WebP (`RIFF....WEBP`)
	if len(data) >= 12 && bytes.HasPrefix(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		isAnimated, width, height, errWebP := inspectRIFFWebPContainer(data)
		if errWebP != nil {
			return nil, "", "", errWebP
		}
		if errDim := validateImageDimensions(width, height); errDim != nil {
			return nil, "", "", errDim
		}
		if isAnimated {
			if int64(len(data)) > variables.MaxArticleAnimatedWebPBytes {
				return nil, "", "", numan_error.NewBadRequest(
					numan_error.CodePayloadTooLarge,
					fmt.Sprintf("La taille du WebP animé dépasse la limite maximale autorisée (%d octets).", variables.MaxArticleAnimatedWebPBytes),
					nil,
				)
			}
			return data, "webp", "image/webp", nil
		}
		// WebP statique -> décodage, redimensionnement homothétique et encodage AVIF
		avifBytes, errAvif := transcodeStaticImageToAVIF(data, maxDim)
		if errAvif != nil {
			return nil, "", "", errAvif
		}
		return avifBytes, "avif", "image/avif", nil
	}

	// 3. Détection JPEG, PNG ou AVIF statique
	isJPEG := len(data) >= 3 && bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF})
	isPNG := len(data) >= 8 && bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	isAVIF := isISOBMFFAVIF(data)

	if !isJPEG && !isPNG && !isAVIF {
		return nil, "", "", numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Format d'image non supporté. Formats acceptés : JPEG, PNG, WebP (statique ou animé), GIF, AVIF.",
			nil,
		)
	}

	avifBytes, errAvif := transcodeStaticImageToAVIF(data, maxDim)
	if errAvif != nil {
		return nil, "", "", errAvif
	}
	return avifBytes, "avif", "image/avif", nil
}

func isISOBMFFAVIF(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	if !bytes.Equal(data[4:8], []byte("ftyp")) {
		return false
	}
	brand := string(data[8:12])
	return brand == "avif" || brand == "avis" || brand == "mif1"
}

func validateImageDimensions(width, height int) error {
	if width <= 0 || height <= 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Les dimensions de l'image sont invalides.",
			fmt.Errorf("invalid dimensions %dx%d", width, height),
		)
	}
	maxPixels := int64(variables.ArticleMaxImagePixels)
	if maxPixels <= 0 {
		maxPixels = int64(variables.MediaMaxPixels)
	}
	if int64(width)*int64(height) > maxPixels {
		return numan_error.NewBadRequest(
			numan_error.CodePayloadTooLarge,
			"La résolution de l'image dépasse la limite maximale autorisée.",
			fmt.Errorf("image resolution %dx%d exceeds max pixels %d", width, height, maxPixels),
		)
	}
	return nil
}

func validateGIFStructure(data []byte) error {
	cfg, errCfg := gif.DecodeConfig(bytes.NewReader(data))
	if errCfg != nil {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"En-tête GIF corrompu ou invalide.",
			errCfg,
		)
	}
	if errDim := validateImageDimensions(cfg.Width, cfg.Height); errDim != nil {
		return errDim
	}
	decodedGIF, errAll := gif.DecodeAll(bytes.NewReader(data))
	if errAll != nil || decodedGIF == nil || len(decodedGIF.Image) == 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le fichier GIF fourni est corrompu ou ne contient aucune trame valide.",
			errAll,
		)
	}
	return nil
}

// inspectRIFFWebPContainer inspecte de manière sécurisée le conteneur RIFF/WEBP pour déterminer s'il est animé
// et extraire ses dimensions sans décodage complet en mémoire.
func inspectRIFFWebPContainer(data []byte) (isAnimated bool, width int, height int, err error) {
	if len(data) < 16 || !bytes.Equal(data[:4], []byte("RIFF")) || !bytes.Equal(data[8:12], []byte("WEBP")) {
		return false, 0, 0, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "En-tête RIFF/WEBP invalide.", nil)
	}

	riffSize := int(binary.LittleEndian.Uint32(data[4:8]))
	if riffSize < 4 || 8+riffSize > len(data) {
		return false, 0, 0, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Conteneur RIFF/WEBP tronqué ou taille incohérente.", nil)
	}

	limit := 8 + riffSize
	offset := 12
	hasValidImageChunk := false

	for offset+8 <= limit {
		chunkTag := string(data[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		if chunkSize < 0 || offset+8+chunkSize > limit {
			return false, 0, 0, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Chunk WebP corrompu ou hors bornes.", nil)
		}
		chunkData := data[offset+8 : offset+8+chunkSize]

		switch chunkTag {
		case "VP8X":
			if len(chunkData) < 10 {
				return false, 0, 0, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Chunk VP8X WebP invalide.", nil)
			}
			flags := chunkData[0]
			if (flags & 0x02) != 0 {
				isAnimated = true
			}
			w := (int(chunkData[4]) | int(chunkData[5])<<8 | int(chunkData[6])<<16) + 1
			h := (int(chunkData[7]) | int(chunkData[8])<<8 | int(chunkData[9])<<16) + 1
			if width == 0 && height == 0 {
				width, height = w, h
			}
			hasValidImageChunk = true

		case "ANIM", "ANMF":
			isAnimated = true
			hasValidImageChunk = true

		case "VP8 ":
			if len(chunkData) >= 10 {
				hasValidImageChunk = true
				if width == 0 && height == 0 && chunkData[3] == 0x9d && chunkData[4] == 0x01 && chunkData[5] == 0x2a {
					width = int(binary.LittleEndian.Uint16(chunkData[6:8])) & 0x3FFF
					height = int(binary.LittleEndian.Uint16(chunkData[8:10])) & 0x3FFF
				}
			}

		case "VP8L":
			if len(chunkData) >= 5 && chunkData[0] == 0x2f {
				hasValidImageChunk = true
				if width == 0 && height == 0 {
					bits := binary.LittleEndian.Uint32(chunkData[1:5])
					width = int(bits&0x3FFF) + 1
					height = int((bits>>14)&0x3FFF) + 1
				}
			}
		}

		paddedSize := chunkSize
		if paddedSize%2 == 1 {
			paddedSize++
		}
		offset += 8 + paddedSize
	}

	if !hasValidImageChunk || width <= 0 || height <= 0 {
		return false, 0, 0, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Structure interne WebP invalide ou dimensions introuvables.", nil)
	}

	return isAnimated, width, height, nil
}

// transcodeStaticImageToAVIF valide la résolution avant décodage, décode l'image statique,
// applique imaging.Fit(img, maxDim, maxDim, imaging.Lanczos) uniquement si une dimension excède maxDim (sans upscaling ni rognage),
// puis encode le résultat en AVIF.
func transcodeStaticImageToAVIF(data []byte, maxDim int) ([]byte, error) {
	var (
		cfg        image.Config
		errCfg     error
		decodedImg image.Image
		errDec     error
	)

	if isISOBMFFAVIF(data) {
		cfg, errCfg = avif.DecodeConfig(bytes.NewReader(data))
	} else {
		cfg, _, errCfg = image.DecodeConfig(bytes.NewReader(data))
	}
	if errCfg != nil {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Le fichier fourni n'est pas une image statique valide ou est corrompu.",
			errCfg,
		)
	}

	if errDim := validateImageDimensions(cfg.Width, cfg.Height); errDim != nil {
		return nil, errDim
	}

	if isISOBMFFAVIF(data) {
		decodedImg, errDec = avif.Decode(bytes.NewReader(data))
	} else {
		decodedImg, _, errDec = image.Decode(bytes.NewReader(data))
	}
	if errDec != nil {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Erreur lors du décodage structurel de l'image.",
			errDec,
		)
	}

	bounds := decodedImg.Bounds()
	if maxDim > 0 && (bounds.Dx() > maxDim || bounds.Dy() > maxDim) {
		decodedImg = imaging.Fit(decodedImg, maxDim, maxDim, imaging.Lanczos)
	}

	var avifBuf bytes.Buffer
	opts := avif.Options{
		Quality: variables.MediaAvifQuality,
		Speed:   variables.MediaAvifSpeed,
	}
	if errEnc := avif.Encode(&avifBuf, decodedImg, opts); errEnc != nil {
		numan_log.Error(context.Background()).Err(errEnc).Msg("Erreur interne lors de l'encodage AVIF de l'asset d'article")
		return nil, numan_error.NewInternal()
	}

	return avifBuf.Bytes(), nil
}
