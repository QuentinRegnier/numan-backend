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
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildUpdateTestRouter(withUser bool, adminID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/admin/articles/update", func(c *gin.Context) {
		if withUser {
			c.Set("userID", adminID)
			c.Set("user_grade", variables.UserGradeAdmin)
		}
		UpdateArticleHandler(c)
	})
	return r
}

func seedUpdateFixtures(t *testing.T) (articleID, mdID, oldCoverID, newCoverID, updatedAt int64) {
	t.Helper()
	now := domain.NowMillis()
	mdID, oldCoverID, newCoverID = 6001, 6002, 6003
	articleID = 6100

	for _, m := range []media_models.MediaPayload{
		{ID: mdID, OwnerID: 9001, StoragePath: "articles/doc.md", Visibility: variables.MediaVisibilityPublic, CreatedAt: now, UpdatedAt: now},
		{ID: oldCoverID, OwnerID: 9001, StoragePath: "articles/old_cover.avif", Visibility: variables.MediaVisibilityPublic, CreatedAt: now, UpdatedAt: now},
		{ID: newCoverID, OwnerID: 9001, StoragePath: "articles/new_cover.avif", Visibility: variables.MediaVisibilityPending, CreatedAt: now, UpdatedAt: now},
	} {
		require.NoError(t, object_cache_service.SetMediaInObjectCache(context.Background(), m))
	}

	require.NoError(t, object_cache_service.SetArticleInCache(context.Background(), article_models.ArticlePayload{
		ID:              articleID,
		Title:           "Titre initial",
		Summary:         "Résumé initial",
		CoverMediaID:    oldCoverID,
		MarkdownMediaID: mdID,
		Visibility:      variables.ArticleVisibilityPublished,
		CreatedAt:       now,
		UpdatedAt:       now,
	}))
	return articleID, mdID, oldCoverID, newCoverID, now
}

func TestUpdateArticleHandler_SuccessOptimisticAndMediaDiff(t *testing.T) {
	t.Setenv("PUBLIC_MEDIA_BASE_URL", "https://cdn.numan.app/public")
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	articleID, _, oldCoverID, newCoverID, expectedTS := seedUpdateFixtures(t)
	router := buildUpdateTestRouter(true, 9001)

	newTitle := "Titre mis à jour"
	body, err := json.Marshal(article_models.UpdateArticleInput{
		ArticleID:         articleID,
		Title:             &newTitle,
		CoverMediaID:      &newCoverID,
		ExpectedUpdatedAt: &expectedTS,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/admin/articles/update", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var out article_models.UpdateArticleOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, newTitle, out.Article.Title)
	assert.Equal(t, "https://cdn.numan.app/public/articles/new_cover.avif", out.Article.CoverURL)
	assert.Greater(t, out.Article.UpdatedAt, expectedTS)
	assert.Contains(t, out.DeactivatedMediaID, oldCoverID)

	_, errOld := object_cache_service.GetMediaFromObjectCache(context.Background(), oldCoverID)
	require.Error(t, errOld)

	var rawOldCover media_models.MediaPayload
	require.NoError(t, redis.Media.GetObject(context.Background(), oldCoverID, &rawOldCover))
	assert.Equal(t, variables.MediaVisibilitySoftDeleted, rawOldCover.Visibility)
}

func TestUpdateArticleHandler_ErrorsAndConflict(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	articleID, _, _, _, _ := seedUpdateFixtures(t)

	t.Run("missing userID returns 401", func(t *testing.T) {
		router := buildUpdateTestRouter(false, 0)
		req := httptest.NewRequest(http.MethodPut, "/admin/articles/update", bytes.NewReader([]byte(`{"article_id":1}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("query parameters rejected with 400", func(t *testing.T) {
		router := buildUpdateTestRouter(true, 9001)
		req := httptest.NewRequest(http.MethodPut, "/admin/articles/update?article_id=123", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid json returns 400", func(t *testing.T) {
		router := buildUpdateTestRouter(true, 9001)
		req := httptest.NewRequest(http.MethodPut, "/admin/articles/update", bytes.NewReader([]byte(`{broken`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing article returns 404", func(t *testing.T) {
		router := buildUpdateTestRouter(true, 9001)
		title := "Absent"
		body, _ := json.Marshal(article_models.UpdateArticleInput{ArticleID: 999999, Title: &title})
		req := httptest.NewRequest(http.MethodPut, "/admin/articles/update", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("stale expected_updated_at returns 409 conflict", func(t *testing.T) {
		router := buildUpdateTestRouter(true, 9001)
		title := "Conflit"
		staleTS := int64(1000)
		body, _ := json.Marshal(article_models.UpdateArticleInput{
			ArticleID:         articleID,
			Title:             &title,
			ExpectedUpdatedAt: &staleTS,
		})
		req := httptest.NewRequest(http.MethodPut, "/admin/articles/update", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusConflict, w.Code)
	})
}
