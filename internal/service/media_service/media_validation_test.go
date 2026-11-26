package media_service_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraMinio "github.com/QuentinRegnier/numan-backend/internal/infrastructure/minio"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dummyPNGReader(t *testing.T) *bytes.Reader {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return bytes.NewReader(buf.Bytes())
}

func TestMediaServices_ValidationAndIAScorePersistence(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	fakeMinio, minioClient := test.SetupTestMinio(t)
	infraMinio.MinioClient = minioClient
	t.Setenv("MINIO_BUCKET_NAME", "test-bucket")
	fakeMinio.SeedObject("test-bucket", "keep", []byte("ok"))

	ctx := context.Background()

	// Invalid UploadMedia inputs never touch Redis
	err := media_service.UploadMediaWithStatusAndIAScore(dummyPNGReader(t), 0, 100, variables.MediaVisibilityPending, 0.1)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)

	err = media_service.UploadMediaWithStatusAndIAScore(dummyPNGReader(t), 10, 0, variables.MediaVisibilityPending, 0.1)
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)

	err = media_service.UploadMediaWithStatusAndIAScore(dummyPNGReader(t), 10, 100, 999, 0.1)
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)

	err = media_service.UploadMediaWithStatusAndIAScore(dummyPNGReader(t), 10, 100, variables.MediaVisibilityPending, math.NaN())
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)

	keys, errKeys := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, errKeys)
	assert.Empty(t, keys)

	// Invalid SignMediaInput never touches Redis
	_, errSign := media_service.GetSignedURLForClient(ctx, 10, media_models.SignMediaInput{
		MediaID:        100,
		PostID:         1,
		ConversationID: 2,
	})
	require.ErrorAs(t, errSign, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, "AMBIGUOUS_CONTEXT", appErr.Code)

	_, errSign = media_service.GetSignedURLForClient(ctx, 10, media_models.SignMediaInput{MediaID: 0})
	require.ErrorAs(t, errSign, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)

	// Valid upload persists IAScore into L1 MediaPayload
	require.NoError(t, media_service.UploadMediaWithStatusAndIAScore(dummyPNGReader(t), 55, 777, variables.MediaVisibilityPending, 0.42))
	cached, errGet := object_cache_service.GetMediaFromObjectCache(ctx, 777)
	require.NoError(t, errGet)
	assert.InDelta(t, 0.42, cached.IAScore, 0.0001)
}
