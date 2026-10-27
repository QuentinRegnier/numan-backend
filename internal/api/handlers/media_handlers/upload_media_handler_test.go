package media_handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/api/handlers/media_handlers"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	infraMinio "github.com/QuentinRegnier/numan-backend/internal/infrastructure/minio"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/logger"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createDummyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for x := 0; x < 10; x++ {
		for y := 0; y < 10; y++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func setupUploadRouter(userID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	r.POST("/media/upload", media_handlers.UploadMediaHandler)
	return r
}

func TestUploadMediaHandler_NewUserGoesToQuarantine(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	fakeMinio, minioClient := test.SetupTestMinio(t)
	fakeMinio.SeedObject("test-bucket", "placeholder", []byte("test"))
	infraMinio.MinioClient = minioClient
	t.Setenv("MINIO_BUCKET_NAME", "test-bucket")

	var logBuf bytes.Buffer
	origLogger := logger.Log
	logger.Log = zerolog.New(&logBuf)
	defer func() {
		logger.Log = origLogger
	}()

	ctx := context.Background()
	newUserID := int64(1001)

	// Nouvel utilisateur : T = 0.20 (< T_seuil = 0.40)
	require.NoError(t, redis.UsersLite.SetObject(ctx, newUserID, lite_models.UserLiteRequest{
		ID:         newUserID,
		Username:   "newbie",
		TrustScore: 0.20,
	}))

	imgBytes := createDummyPNG(t)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "image.png")
	require.NoError(t, err)
	_, err = part.Write(imgBytes)
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("ia_score", "0.15"))
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/media/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	r := setupUploadRouter(newUserID)
	r.ServeHTTP(w, req)

	t.Logf("Response body: %s", w.Body.String())
	t.Logf("Captured logs: %s", logBuf.String())
	assert.Equal(t, http.StatusAccepted, w.Code)

	var output media_models.UploadMediaOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &output))
	assert.Greater(t, output.MediaID, int64(0))
	assert.Equal(t, variables.MediaVisibilityQuarantine, output.Status, "Nouvel utilisateur doit avoir le statut Quarantaine (2)")
	assert.True(t, output.RequiresModeration)
	assert.NotEmpty(t, output.Message)

	// Vérification de la persistance en cache avec Visibility = 2
	cachedMedia, errGet := object_cache_service.GetMediaFromObjectCache(ctx, output.MediaID)
	require.NoError(t, errGet)
	assert.Equal(t, variables.MediaVisibilityQuarantine, int(cachedMedia.Visibility))
}

func TestUploadMediaHandler_EstablishedUserPending(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	fakeMinio, minioClient := test.SetupTestMinio(t)
	fakeMinio.SeedObject("test-bucket", "placeholder", []byte("test"))
	infraMinio.MinioClient = minioClient
	t.Setenv("MINIO_BUCKET_NAME", "test-bucket")

	ctx := context.Background()
	estUserID := int64(2002)

	// Utilisateur de confiance : T = 0.85 (>= T_seuil = 0.40)
	require.NoError(t, redis.UsersLite.SetObject(ctx, estUserID, lite_models.UserLiteRequest{
		ID:         estUserID,
		Username:   "trusted_user",
		TrustScore: 0.85,
	}))

	imgBytes := createDummyPNG(t)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "image.png")
	require.NoError(t, err)
	_, err = part.Write(imgBytes)
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("ia_score", "0.02"))
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/media/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	r := setupUploadRouter(estUserID)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)

	var output media_models.UploadMediaOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &output))
	assert.Greater(t, output.MediaID, int64(0))
	assert.Equal(t, variables.MediaVisibilityPending, output.Status, "Utilisateur de confiance doit avoir le statut Pending (0)")
	assert.False(t, output.RequiresModeration)

	// Vérification de la persistance en cache avec Visibility = 0
	cachedMedia, errGet := object_cache_service.GetMediaFromObjectCache(ctx, output.MediaID)
	require.NoError(t, errGet)
	assert.Equal(t, variables.MediaVisibilityPending, int(cachedMedia.Visibility))
}

func TestUploadMediaHandler_InvalidIAScore(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	imgBytes := createDummyPNG(t)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "image.png")
	require.NoError(t, err)
	_, err = part.Write(imgBytes)
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("ia_score", "1.50")) // Score hors bornes [0.0, 1.0]
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/media/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	r := setupUploadRouter(3003)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
