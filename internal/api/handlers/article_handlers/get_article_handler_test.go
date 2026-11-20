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

func buildGetTestRouter(withUser bool, callerID int64, grade int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/articles/get", func(c *gin.Context) {
		if withUser {
			c.Set("userID", callerID)
			c.Set("user_grade", grade)
		}
		GetArticleHandler(c)
	})
	return r
}

func seedGetFixtures(t *testing.T) {
	t.Helper()
	now := domain.NowMillis()
	pastExp := now - int64(time.Hour/time.Millisecond)

	for _, m := range []media_models.MediaPayload{
		{ID: 8001, OwnerID: 9001, StoragePath: "articles/pub.md", Visibility: variables.MediaVisibilityPublic, CreatedAt: now, UpdatedAt: now},
		{ID: 8002, OwnerID: 9001, StoragePath: "articles/pub_cover.avif", Visibility: variables.MediaVisibilityPublic, CreatedAt: now, UpdatedAt: now},
		{ID: 8003, OwnerID: 9001, StoragePath: "articles/draft.md", Visibility: variables.MediaVisibilityPublic, CreatedAt: now, UpdatedAt: now},
	} {
		require.NoError(t, object_cache_service.SetMediaInObjectCache(context.Background(), m))
	}

	require.NoError(t, object_cache_service.SetArticleInCache(context.Background(), article_models.ArticlePayload{
		ID:                 8101,
		Title:              "Article Publié",
		Summary:            "Résumé public",
		CoverMediaID:       8002,
		MarkdownMediaID:    8001,
		AttachmentMediaIDs: []int64{8002},
		PartnerURL:         "https://numan.app",
		Visibility:         variables.ArticleVisibilityPublished,
		CreatedAt:          now,
		UpdatedAt:          now,
	}))

	require.NoError(t, object_cache_service.SetArticleInCache(context.Background(), article_models.ArticlePayload{
		ID:              8102,
		Title:           "Article Brouillon",
		Summary:         "Interne",
		MarkdownMediaID: 8003,
		Visibility:      variables.ArticleVisibilityDraft,
		CreatedAt:       now + 1,
		UpdatedAt:       now + 1,
	}))

	require.NoError(t, object_cache_service.SetArticleInCache(context.Background(), article_models.ArticlePayload{
		ID:              8103,
		Title:           "Article Expiré",
		MarkdownMediaID: 8001,
		Visibility:      variables.ArticleVisibilityPublished,
		ExpiresAt:       &pastExp,
		CreatedAt:       now + 2,
		UpdatedAt:       now + 2,
	}))
}

func TestGetArticleHandler_VisibilityAndURLHydration(t *testing.T) {
	t.Setenv("PUBLIC_MEDIA_BASE_URL", "https://cdn.numan.app/public")
	_ = test.SetupTestRedis(t)
	seedGetFixtures(t)

	userRouter := buildGetTestRouter(true, 1001, variables.UserGradeUser)
	adminRouter := buildGetTestRouter(true, 9001, variables.UserGradeAdmin)

	t.Run("regular user reads published article with public URLs and no raw markdown", func(t *testing.T) {
		body, _ := json.Marshal(article_models.GetArticleInput{ArticleID: 8101})
		req := httptest.NewRequest(http.MethodPost, "/articles/get", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		userRouter.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var out article_models.GetArticleOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		assert.Equal(t, int64(8101), out.Article.ID)
		assert.Equal(t, "https://cdn.numan.app/public/articles/pub.md", out.Article.MarkdownURL)
		assert.Equal(t, "https://cdn.numan.app/public/articles/pub_cover.avif", out.Article.CoverURL)
		assert.Len(t, out.Article.Attachments, 1)
	})

	t.Run("regular user gets 404 on draft and expired articles", func(t *testing.T) {
		for _, id := range []int64{8102, 8103} {
			body, _ := json.Marshal(article_models.GetArticleInput{ArticleID: id})
			req := httptest.NewRequest(http.MethodPost, "/articles/get", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			userRouter.ServeHTTP(w, req)
			assert.Equal(t, http.StatusNotFound, w.Code)
		}
	})

	t.Run("admin gets 200 on draft and expired articles", func(t *testing.T) {
		for _, id := range []int64{8102, 8103} {
			body, _ := json.Marshal(article_models.GetArticleInput{ID: id})
			req := httptest.NewRequest(http.MethodPost, "/articles/get", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			adminRouter.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}
	})
}

func TestGetArticleHandler_ValidationAndAuth(t *testing.T) {
	_ = test.SetupTestRedis(t)

	t.Run("missing userID returns 401", func(t *testing.T) {
		router := buildGetTestRouter(false, 0, 0)
		req := httptest.NewRequest(http.MethodPost, "/articles/get", bytes.NewReader([]byte(`{"article_id":1}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("query parameters rejected with 400", func(t *testing.T) {
		router := buildGetTestRouter(true, 1001, variables.UserGradeUser)
		req := httptest.NewRequest(http.MethodPost, "/articles/get?article_id=1", bytes.NewReader([]byte(`{"article_id":1}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid article_id <= 0 returns 400", func(t *testing.T) {
		router := buildGetTestRouter(true, 1001, variables.UserGradeUser)
		req := httptest.NewRequest(http.MethodPost, "/articles/get", bytes.NewReader([]byte(`{"article_id":0}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
