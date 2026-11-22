package minio

import (
	"context"
	"os"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinioClient est la variable globale qui sera utilisée par tes fonctions UploadMedia
var MinioClient *minio.Client

// MinioConfig regroupe la configuration résolue pour l'initialisation du client S3/MinIO.
type MinioConfig struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	BucketName      string
	UseSSL          bool
}

// ResolveMinioConfig résout la configuration MinIO depuis l'environnement.
// Priorité des credentials (principe du moindre privilège K8s -> fallback dev Compose) :
// 1. S3_ACCESS_KEY / S3_SECRET_KEY (utilisateur applicatif restreint injecté par api.yaml / worker.yaml)
// 2. MINIO_ACCESS_KEY / MINIO_SECRET_KEY (convention service watermark)
// 3. MINIO_ROOT_USER / MINIO_ROOT_PASSWORD (fallback local docker-compose.dev.yml)
func ResolveMinioConfig() MinioConfig {
	endpoint := strings.TrimSpace(os.Getenv("MINIO_ENDPOINT"))

	accessKeyID := firstNonEmptyEnv("S3_ACCESS_KEY", "MINIO_ACCESS_KEY", "MINIO_ROOT_USER")
	secretAccessKey := firstNonEmptyEnv("S3_SECRET_KEY", "MINIO_SECRET_KEY", "MINIO_ROOT_PASSWORD")

	bucketName := strings.TrimSpace(os.Getenv("MINIO_BUCKET_NAME"))
	if bucketName == "" {
		bucketName = "numan-bucket"
	}

	useSSL := strings.EqualFold(firstNonEmptyEnv("S3_USE_SSL", "MINIO_USE_SSL", "USE_SSL"), "true")

	return MinioConfig{
		Endpoint:        endpoint,
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		BucketName:      bucketName,
		UseSSL:          useSSL,
	}
}

func firstNonEmptyEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func InitMinio() {
	cfg := ResolveMinioConfig()

	var err error
	MinioClient, err = minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		numan_log.Fatal(context.Background()).Err(err).Msg("Impossible d'initialiser le client MinIO")
	}

	// Test de connexion (Ping)
	exists, err := MinioClient.BucketExists(context.Background(), cfg.BucketName)
	if err != nil {
		numan_log.Warn(context.Background()).Err(err).Str("bucket", cfg.BucketName).Msg("Connexion MinIO établie, mais impossible de vérifier le bucket")
	} else if !exists {
		numan_log.Warn(context.Background()).Str("bucket", cfg.BucketName).Msg("Le bucket n'existe pas encore.")
	} else {
		numan_log.Info(context.Background()).Str("bucket", cfg.BucketName).Bool("ssl", cfg.UseSSL).Msg("Connexion MinIO réussie et bucket vérifié")
	}
}
