package media_service

import (
	"context"
	"strings"
	"time"

	infraMinio "github.com/QuentinRegnier/numan-backend/internal/infrastructure/minio"
)

// CleanupAbortedMultipartUploads purge les uploads multipart S3/MinIO abandonnés depuis plus de maxAge.
func CleanupAbortedMultipartUploads(ctx context.Context, maxAge time.Duration, maxItems int) (aborted int64, err error) {
	if infraMinio.MinioClient == nil {
		return 0, nil
	}
	cfg := infraMinio.ResolveMinioConfig()
	bucket := strings.TrimSpace(cfg.BucketName)
	if bucket == "" {
		return 0, nil
	}
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	if maxItems <= 0 {
		maxItems = 200
	}

	cutoff := time.Now().Add(-maxAge)
	for upload := range infraMinio.MinioClient.ListIncompleteUploads(ctx, bucket, "", true) {
		if ctx.Err() != nil {
			return aborted, ctx.Err()
		}
		if upload.Err != nil {
			break
		}
		if upload.Initiated.Before(cutoff) {
			if errRm := infraMinio.MinioClient.RemoveIncompleteUpload(ctx, bucket, upload.Key); errRm == nil {
				aborted++
			}
			if int(aborted) >= maxItems {
				break
			}
		}
	}
	return aborted, nil
}
