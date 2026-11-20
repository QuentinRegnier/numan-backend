package article_handlers

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	infraMinio "github.com/QuentinRegnier/numan-backend/internal/infrastructure/minio"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildUploadTestRouter(withUser bool, adminID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/admin/articles/media/upload", func(c *gin.Context) {
		if withUser {
			c.Set("userID", adminID)
			c.Set("user_grade", variables.UserGradeAdmin)
		}
		UploadArticleAssetHandler(c)
	})
	return r
}

func makeUploadMultipartRequest(t *testing.T, urlPath, filename string, content []byte, quality string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	fw, err := mw.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)

	if quality != "" {
		require.NoError(t, mw.WriteField("quality", quality))
	}
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, urlPath, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func encodeUploadTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: 40, G: 120, B: 220, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestUploadArticleAssetHandler_SuccessMarkdownAndAVIF(t *testing.T) {
	t.Setenv("PUBLIC_MEDIA_BASE_URL", "https://cdn.numan.app/public")
	t.Setenv("MINIO_PUBLIC_BUCKET_NAME", "numan-public")

	_ = test.SetupTestRedis(t)
	_, minioClient := test.SetupTestMinio(t)
	infraMinio.MinioClient = minioClient
	_ = pkg.InitSnowflake(1)

	router := buildUploadTestRouter(true, 9001)

	// 1. Upload Markdown asset (auto-detected via .md suffix)
	mdContent := []byte("# Café de Flore\n\nUne institution littéraire de Saint-Germain-des-Prés.\n")
	reqMD := makeUploadMultipartRequest(t, "/admin/articles/media/upload", "flore.md", mdContent, "")
	wMD := httptest.NewRecorder()
	router.ServeHTTP(wMD, reqMD)
	require.Equal(t, http.StatusOK, wMD.Code)

	var mdOut media_models.UploadAdminArticleAssetOutput
	require.NoError(t, json.Unmarshal(wMD.Body.Bytes(), &mdOut))
	assert.NotZero(t, mdOut.MediaID)
	assert.True(t, strings.HasPrefix(mdOut.StoragePath, "articles/"))
	assert.True(t, strings.HasSuffix(mdOut.StoragePath, ".md"))
	assert.Equal(t, "https://cdn.numan.app/public/"+mdOut.StoragePath, mdOut.URL)
	assert.Equal(t, "text/markdown; charset=utf-8", mdOut.ContentType)

	// 2. Upload Cover Image asset (PNG -> AVIF high quality 2048px)
	pngBytes := encodeUploadTestPNG(t, 320, 200)
	reqCover := makeUploadMultipartRequest(t, "/admin/articles/media/upload", "cover.png", pngBytes, "high")
	wCover := httptest.NewRecorder()
	router.ServeHTTP(wCover, reqCover)
	require.Equal(t, http.StatusOK, wCover.Code)

	var coverOut media_models.UploadAdminArticleAssetOutput
	require.NoError(t, json.Unmarshal(wCover.Body.Bytes(), &coverOut))
	assert.NotZero(t, coverOut.MediaID)
	assert.True(t, strings.HasPrefix(coverOut.StoragePath, "articles/"))
	assert.True(t, strings.HasSuffix(coverOut.StoragePath, ".avif"))
	assert.Equal(t, "image/avif", coverOut.ContentType)
}

func TestUploadArticleAssetHandler_ValidationAndAuth(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_, minioClient := test.SetupTestMinio(t)
	infraMinio.MinioClient = minioClient
	_ = pkg.InitSnowflake(1)

	t.Run("missing userID in context returns 401", func(t *testing.T) {
		router := buildUploadTestRouter(false, 0)
		req := makeUploadMultipartRequest(t, "/admin/articles/media/upload", "doc.md", []byte("# Doc"), "")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("query parameters rejected with 400", func(t *testing.T) {
		router := buildUploadTestRouter(true, 9001)
		req := makeUploadMultipartRequest(t, "/admin/articles/media/upload?quality=high", "doc.md", []byte("# Doc"), "")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing file field returns 400", func(t *testing.T) {
		router := buildUploadTestRouter(true, 9001)
		req := httptest.NewRequest(http.MethodPost, "/admin/articles/media/upload", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("unsafe html/script payload in markdown returns 400", func(t *testing.T) {
		router := buildUploadTestRouter(true, 9001)
		req := makeUploadMultipartRequest(t, "/admin/articles/media/upload", "bad.md", []byte("<script>alert(1)</script>"), "")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
