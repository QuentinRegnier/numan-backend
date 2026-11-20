package article_handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildCreateTestRouter(withUser bool, adminID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/admin/articles/create", func(c *gin.Context) {
		if withUser {
			c.Set("userID", adminID)
			c.Set("user_grade", variables.UserGradeAdmin)
		}
		CreateArticleHandler(c)
	})
	return r
}

func seedMediaL1(t *testing.T, id, ownerID int64, storagePath string, vis int) {
	t.Helper()
	now := domain.NowMillis()
	err := object_cache_service.SetMediaInObjectCache(context.Background(), media_models.MediaPayload{
		ID:          id,
		OwnerID:     ownerID,
		StoragePath: storagePath,
		Visibility:  vis,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	require.NoError(t, err)
}

func TestCreateArticleHandler_SuccessPublishedAndDraft(t *testing.T) {
	t.Setenv("PUBLIC_MEDIA_BASE_URL", "https://cdn.numan.app/public")
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	mdID := int64(5001)
	coverID := int64(5002)
	seedMediaL1(t, mdID, 9001, "articles/flore.md", variables.MediaVisibilityPending)
	seedMediaL1(t, coverID, 9002, "articles/cover.avif", variables.MediaVisibilityPending) // cross-admin media

	router := buildCreateTestRouter(true, 9001)

	visPub := variables.ArticleVisibilityPublished
	body, err := json.Marshal(article_models.CreateArticleInput{
		Title:              "Café de Flore",
		Summary:            "Guide complet du célèbre café parisien.",
		CoverMediaID:       coverID,
		MarkdownMediaID:    mdID,
		AttachmentMediaIDs: []int64{coverID},
		PartnerURL:         "https://cafedeflore.fr",
		Visibility:         &visPub,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/admin/articles/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var out article_models.CreateArticleOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.NotZero(t, out.Article.ID)
	assert.Equal(t, "https://cdn.numan.app/public/articles/flore.md", out.Article.MarkdownURL)
	assert.Equal(t, "https://cdn.numan.app/public/articles/cover.avif", out.Article.CoverURL)
	assert.Len(t, out.Article.Attachments, 1)
	assert.Equal(t, variables.ArticleVisibilityPublished, out.Article.Visibility)

	// Vérifie que les médias ont été activés en Public (1)
	mdAfter, errGet := object_cache_service.GetMediaFromObjectCache(context.Background(), mdID)
	require.NoError(t, errGet)
	assert.Equal(t, variables.MediaVisibilityPublic, mdAfter.Visibility)
}

func TestCreateArticleHandler_ValidationAndSecurityErrors(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	t.Run("missing userID returns 401", func(t *testing.T) {
		router := buildCreateTestRouter(false, 0)
		req := httptest.NewRequest(http.MethodPost, "/admin/articles/create", bytes.NewReader([]byte(`{"title":"T","markdown_media_id":1}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("query parameters rejected with 400", func(t *testing.T) {
		router := buildCreateTestRouter(true, 9001)
		req := httptest.NewRequest(http.MethodPost, "/admin/articles/create?status=1", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid json payload returns 400", func(t *testing.T) {
		router := buildCreateTestRouter(true, 9001)
		req := httptest.NewRequest(http.MethodPost, "/admin/articles/create", bytes.NewReader([]byte(`{invalid`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("private user media prefix returns 403", func(t *testing.T) {
		privateMD := int64(5099)
		seedMediaL1(t, privateMD, 9001, "users/9001/secret.md", variables.MediaVisibilityPublic)

		router := buildCreateTestRouter(true, 9001)
		body, _ := json.Marshal(article_models.CreateArticleInput{
			Title:           "Privé interdit",
			MarkdownMediaID: privateMD,
		})
		req := httptest.NewRequest(http.MethodPost, "/admin/articles/create", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}
