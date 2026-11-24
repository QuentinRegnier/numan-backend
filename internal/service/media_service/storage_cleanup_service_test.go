package media_service

import (
	"context"
	"testing"
	"time"

	infraMinio "github.com/QuentinRegnier/numan-backend/internal/infrastructure/minio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupAbortedMultipartUploads_NilClientSafe(t *testing.T) {
	orig := infraMinio.MinioClient
	infraMinio.MinioClient = nil
	t.Cleanup(func() { infraMinio.MinioClient = orig })

	aborted, err := CleanupAbortedMultipartUploads(context.Background(), 24*time.Hour, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(0), aborted)
}
