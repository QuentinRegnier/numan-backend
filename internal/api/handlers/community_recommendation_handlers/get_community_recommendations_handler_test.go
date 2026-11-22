package community_recommendation_handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/api/handlers/community_recommendation_handlers"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCommunityRecommendationsHandler(t *testing.T) {
	test.SetupTestRedis(t)
	ctx := context.Background()

	newRouter := func(userID int64) *gin.Engine {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		if userID > 0 {
			r.Use(func(c *gin.Context) {
				c.Set("userID", userID)
				c.Next()
			})
		}
		r.POST("/communities/recommendations", community_recommendation_handlers.GetCommunityRecommendationsHandler)
		return r
	}

	conv := conversation_models.ConversationPayload{
		ID:          3001,
		Type:        variables.ConversationTypeCommunityPub,
		Title:       "Go France",
		Description: "Communauté Go FR",
		State:       variables.ConversationStateActive,
		Lang:        "fr",
		IsPartner:   true,
	}
	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, conv))
	require.NoError(t, redis.ConvMeta.SetObject(ctx, int64(3001), service.ToConvLiteRequest(conv)))
	require.NoError(t, cache_service.ReplaceRankedCommunitiesByLang(ctx, "fr", []cache_service.RankedCommunityEntry{
		{ConversationID: 3001, Score: 12.5},
	}))

	t.Run("401 when unauthenticated", func(t *testing.T) {
		r := newRouter(0)
		req := httptest.NewRequest(http.MethodPost, "/communities/recommendations", bytes.NewBufferString(`{}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("400 on malformed JSON", func(t *testing.T) {
		r := newRouter(42)
		req := httptest.NewRequest(http.MethodPost, "/communities/recommendations", bytes.NewBufferString(`{bad json`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400 on unsupported language", func(t *testing.T) {
		r := newRouter(42)
		req := httptest.NewRequest(http.MethodPost, "/communities/recommendations", bytes.NewBufferString(`{"lang":"zz"}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("200 on empty body uses defaults and omits is_joined", func(t *testing.T) {
		r := newRouter(42)
		req := httptest.NewRequest(http.MethodPost, "/communities/recommendations", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.NotContains(t, w.Body.String(), "is_joined")

		var resp community_recommendation_models.GetCommunityRecommendationsOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "fr", resp.Lang)
		assert.Equal(t, 1, resp.Total)
		require.Len(t, resp.Communities, 1)
		assert.Equal(t, int64(3001), resp.Communities[0].ID)
		assert.True(t, resp.Communities[0].IsPartner)
		assert.Equal(t, 0, resp.Communities[0].PinSlot)
	})
}
