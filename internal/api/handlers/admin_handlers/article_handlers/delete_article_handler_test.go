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

func buildDeleteTestRouter(withUser bool, adminID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.DELETE("/admin/articles/delete", func(c *gin.Context) {
		if withUser {
			c.Set("userID", adminID)
			c.Set("user_grade", variables.UserGradeAdmin)
		}
		DeleteArticleHandler(c)
	})
	return r
}

func seedDeleteArticle(t *testing.T, articleID, mdID int64) int64 {
	t.Helper()
	now := domain.NowMillis()
	require.NoError(t, object_cache_service.SetMediaInObjectCache(context.Background(), media_models.MediaPayload{
		ID:          mdID,
		OwnerID:     9001,
		StoragePath: "articles/delete_doc.md",
		Visibility:  variables.MediaVisibilityPublic,
		CreatedAt:   now,
		UpdatedAt:   now,
	}))
	require.NoError(t, object_cache_service.SetArticleInCache(context.Background(), article_models.ArticlePayload{
		ID:              articleID,
		Title:           "À supprimer",
		MarkdownMediaID: mdID,
		Visibility:      variables.ArticleVisibilityPublished,
		CreatedAt:       now,
		UpdatedAt:       now,
	}))
	return now
}

func TestDeleteArticleHandler_SingleAndBatchSuccess(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	ts1 := seedDeleteArticle(t, 7101, 7001)
	_ = seedDeleteArticle(t, 7102, 7002)
	_ = seedDeleteArticle(t, 7103, 7003)

	router := buildDeleteTestRouter(true, 9001)

	// 1. Single delete with expected_updated_at -> 200 OK
	singleBody, _ := json.Marshal(article_models.DeleteArticleInput{
		ArticleID:         7101,
		ExpectedUpdatedAt: &ts1,
	})
	req1 := httptest.NewRequest(http.MethodDelete, "/admin/articles/delete", bytes.NewReader(singleBody))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code)

	var out1 article_models.DeleteArticleOutput
	require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &out1))
	assert.Equal(t, 1, out1.DeletedCount)
	assert.Equal(t, []int64{7101}, out1.ArticleIDs)
	assert.Contains(t, out1.DeactivatedMediaID, int64(7001))

	_, errMissing := object_cache_service.GetArticleFromCache(context.Background(), 7101)
	assert.Error(t, errMissing)

	// 2. Batch delete (including one unknown ID gracefully ignored) -> 200 OK
	batchBody, _ := json.Marshal(article_models.DeleteArticleInput{
		ArticleIDs: []int64{7102, 7103, 999999},
	})
	req2 := httptest.NewRequest(http.MethodDelete, "/admin/articles/delete", bytes.NewReader(batchBody))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	var out2 article_models.DeleteArticleOutput
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &out2))
	assert.Equal(t, 2, out2.DeletedCount)
	assert.ElementsMatch(t, []int64{7102, 7103}, out2.ArticleIDs)
}

func TestDeleteArticleHandler_ValidationAndConflictErrors(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	_ = seedDeleteArticle(t, 7201, 7011)

	t.Run("missing userID returns 401", func(t *testing.T) {
		router := buildDeleteTestRouter(false, 0)
		req := httptest.NewRequest(http.MethodDelete, "/admin/articles/delete", bytes.NewReader([]byte(`{"article_id":7201}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("query parameters rejected with 400", func(t *testing.T) {
		router := buildDeleteTestRouter(true, 9001)
		req := httptest.NewRequest(http.MethodDelete, "/admin/articles/delete?article_id=7201", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("expected_updated_at with batch IDs returns 400", func(t *testing.T) {
		router := buildDeleteTestRouter(true, 9001)
		ts := int64(12345)
		body, _ := json.Marshal(article_models.DeleteArticleInput{
			ArticleIDs:        []int64{7201, 7202},
			ExpectedUpdatedAt: &ts,
		})
		req := httptest.NewRequest(http.MethodDelete, "/admin/articles/delete", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("single missing article returns 404", func(t *testing.T) {
		router := buildDeleteTestRouter(true, 9001)
		body, _ := json.Marshal(article_models.DeleteArticleInput{ArticleID: 888888})
		req := httptest.NewRequest(http.MethodDelete, "/admin/articles/delete", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("stale expected_updated_at returns 409 conflict", func(t *testing.T) {
		router := buildDeleteTestRouter(true, 9001)
		stale := int64(1000)
		body, _ := json.Marshal(article_models.DeleteArticleInput{
			ArticleID:         7201,
			ExpectedUpdatedAt: &stale,
		})
		req := httptest.NewRequest(http.MethodDelete, "/admin/articles/delete", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusConflict, w.Code)
	})
}
