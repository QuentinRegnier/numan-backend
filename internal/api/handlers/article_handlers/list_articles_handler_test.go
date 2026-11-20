package article_handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildListTestRouter(withUser bool, callerID int64, grade int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/articles/list", func(c *gin.Context) {
		if withUser {
			c.Set("userID", callerID)
			c.Set("user_grade", grade)
		}
		ListArticlesHandler(c)
	})
	return r
}

func seedListFixtures(t *testing.T) {
	t.Helper()
	now := domain.NowMillis()
	pastExp := now - int64(time.Hour/time.Millisecond)

	for _, m := range []media_models.MediaPayload{
		{ID: 8201, OwnerID: 9001, StoragePath: "articles/doc.md", Visibility: variables.MediaVisibilityPublic, CreatedAt: now, UpdatedAt: now},
		{ID: 8202, OwnerID: 9001, StoragePath: "articles/cover1.avif", Visibility: variables.MediaVisibilityPublic, CreatedAt: now, UpdatedAt: now},
	} {
		require.NoError(t, object_cache_service.SetMediaInObjectCache(context.Background(), m))
	}

	require.NoError(t, object_cache_service.SetArticleInCache(context.Background(), article_models.ArticlePayload{
		ID:              8301,
		Title:           "Publié 1",
		CoverMediaID:    8202,
		MarkdownMediaID: 8201,
		Visibility:      variables.ArticleVisibilityPublished,
		CreatedAt:       now,
		UpdatedAt:       now,
	}))
	require.NoError(t, object_cache_service.SetArticleInCache(context.Background(), article_models.ArticlePayload{
		ID:              8302,
		Title:           "Publié 2",
		MarkdownMediaID: 8201,
		Visibility:      variables.ArticleVisibilityPublished,
		CreatedAt:       now + 10,
		UpdatedAt:       now + 10,
	}))
	require.NoError(t, object_cache_service.SetArticleInCache(context.Background(), article_models.ArticlePayload{
		ID:              8303,
		Title:           "Brouillon",
		MarkdownMediaID: 8201,
		Visibility:      variables.ArticleVisibilityDraft,
		CreatedAt:       now + 20,
		UpdatedAt:       now + 20,
	}))
	require.NoError(t, object_cache_service.SetArticleInCache(context.Background(), article_models.ArticlePayload{
		ID:              8304,
		Title:           "Expiré",
		MarkdownMediaID: 8201,
		Visibility:      variables.ArticleVisibilityPublished,
		ExpiresAt:       &pastExp,
		CreatedAt:       now + 30,
		UpdatedAt:       now + 30,
	}))
}

func TestListArticlesHandler_RoleFilteringAndPagination(t *testing.T) {
	t.Setenv("PUBLIC_MEDIA_BASE_URL", "https://cdn.numan.app/public")
	_ = test.SetupTestRedis(t)
	seedListFixtures(t)

	userRouter := buildListTestRouter(true, 1001, variables.UserGradeUser)
	adminRouter := buildListTestRouter(true, 9001, variables.UserGradeAdmin)

	t.Run("regular user with empty body sees only published non-expired articles without markdown_url", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/articles/list", nil)
		w := httptest.NewRecorder()
		userRouter.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var out article_models.GetArticlesOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		assert.Equal(t, 2, out.Total)
		require.Len(t, out.Articles, 2)
		assert.Equal(t, int64(8302), out.Articles[0].ID)
		assert.Equal(t, int64(8301), out.Articles[1].ID)
		assert.Equal(t, "https://cdn.numan.app/public/articles/cover1.avif", out.Articles[1].CoverURL)
		assert.NotContains(t, w.Body.String(), "markdown_url")
	})

	t.Run("admin sees published, draft and expired articles with pagination", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/articles/list", bytes.NewReader([]byte(`{"limit":2,"offset":1}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		adminRouter.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var out article_models.GetArticlesOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		assert.Equal(t, 4, out.Total)
		assert.Equal(t, 2, out.Limit)
		assert.Equal(t, 1, out.Offset)
		require.Len(t, out.Articles, 2)
		assert.Equal(t, int64(8303), out.Articles[0].ID)
		assert.Equal(t, int64(8302), out.Articles[1].ID)
	})
}

func TestListArticlesHandler_ValidationAndAuth(t *testing.T) {
	_ = test.SetupTestRedis(t)

	t.Run("missing userID returns 401", func(t *testing.T) {
		router := buildListTestRouter(false, 0, 0)
		req := httptest.NewRequest(http.MethodPost, "/articles/list", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("query parameters rejected with 400", func(t *testing.T) {
		router := buildListTestRouter(true, 1001, variables.UserGradeUser)
		req := httptest.NewRequest(http.MethodPost, "/articles/list?limit=10", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("malformed JSON returns 400", func(t *testing.T) {
		router := buildListTestRouter(true, 1001, variables.UserGradeUser)
		req := httptest.NewRequest(http.MethodPost, "/articles/list", bytes.NewReader([]byte(`{invalid`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("negative limit returns 400", func(t *testing.T) {
		router := buildListTestRouter(true, 1001, variables.UserGradeUser)
		req := httptest.NewRequest(http.MethodPost, "/articles/list", bytes.NewReader([]byte(`{"limit":-1}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
