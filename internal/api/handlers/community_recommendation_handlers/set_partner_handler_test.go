package community_recommendation_handlers_test

import (
	"bytes"
	"context"
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/api/handlers/community_recommendation_handlers"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetPartnerHandler(t *testing.T) {
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
		r.POST("/admin/communities/partner", community_recommendation_handlers.SetPartnerHandler)
		return r
	}

	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(1), lite_models.UserLiteRequest{ID: 1, Username: "admin", Grade: variables.UserGradeAdmin}))
	require.NoError(t, redis.UsersLite.SetObject(ctx, int64(2), lite_models.UserLiteRequest{ID: 2, Username: "user", Grade: variables.UserGradeUser}))

	conv := conversation_models.ConversationPayload{
		ID:    4001,
		Type:  variables.ConversationTypeCommunityPub,
		Title: "Numan Official",
		State: variables.ConversationStateActive,
		Lang:  "fr",
	}
	require.NoError(t, object_cache_service.SetConversationInObjectCache(ctx, conv))
	require.NoError(t, redis.ConvMeta.SetObject(ctx, int64(4001), service.ToConvLiteRequest(conv)))

	t.Run("401 when unauthenticated", func(t *testing.T) {
		r := newRouter(0)
		req := httptest.NewRequest(http.MethodPost, "/admin/communities/partner", bytes.NewBufferString(`{"conversation_id":4001,"is_partner":true}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("403 for non-admin", func(t *testing.T) {
		r := newRouter(2)
		req := httptest.NewRequest(http.MethodPost, "/admin/communities/partner", bytes.NewBufferString(`{"conversation_id":4001,"is_partner":true}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("400 when conversation_id is missing", func(t *testing.T) {
		r := newRouter(1)
		req := httptest.NewRequest(http.MethodPost, "/admin/communities/partner", bytes.NewBufferString(`{"is_partner":true}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("200 sets partner false and true cleanly", func(t *testing.T) {
		r := newRouter(1)
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "func_set_community_partner") {
				return test.NewMockRows(
					[]string{"status_code", "conv_lang", "is_partner"},
					[][]driver.Value{{"OK", "fr", args[1].Value}},
				), nil
			}
			return test.NewMockRows([]string{"id"}, nil), nil
		}, nil)

		reqFalse := httptest.NewRequest(http.MethodPost, "/admin/communities/partner", bytes.NewBufferString(`{"conversation_id":4001,"is_partner":false}`))
		wFalse := httptest.NewRecorder()
		r.ServeHTTP(wFalse, reqFalse)
		require.Equal(t, http.StatusOK, wFalse.Code)
		assert.Contains(t, wFalse.Body.String(), `"is_partner":false`)

		reqTrue := httptest.NewRequest(http.MethodPost, "/admin/communities/partner", bytes.NewBufferString(`{"conversation_id":4001,"is_partner":true}`))
		wTrue := httptest.NewRecorder()
		r.ServeHTTP(wTrue, reqTrue)
		require.Equal(t, http.StatusOK, wTrue.Code)
		assert.Contains(t, wTrue.Body.String(), `"is_partner":true`)
	})
}
