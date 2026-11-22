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

func TestPinCommunityHandler(t *testing.T) {
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
		r.POST("/admin/communities/pin", community_recommendation_handlers.PinCommunityHandler)
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
		req := httptest.NewRequest(http.MethodPost, "/admin/communities/pin", bytes.NewBufferString(`{"conversation_id":4001,"slot":1}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("403 for non-admin", func(t *testing.T) {
		r := newRouter(2)
		req := httptest.NewRequest(http.MethodPost, "/admin/communities/pin", bytes.NewBufferString(`{"conversation_id":4001,"slot":1}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("200 on success and 409 on conflict", func(t *testing.T) {
		r := newRouter(1)

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "func_set_community_pin") {
				return test.NewMockRows(
					[]string{"status_code", "conv_lang", "old_pin_slot", "displaced_conv_id"},
					[][]driver.Value{{"OK", "fr", nil, nil}},
				), nil
			}
			return test.NewMockRows([]string{"id"}, nil), nil
		}, nil)

		req := httptest.NewRequest(http.MethodPost, "/admin/communities/pin", bytes.NewBufferString(`{"conversation_id":4001,"slot":1}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.NotContains(t, w.Body.String(), "displaced_conversation_id")

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(
				[]string{"status_code", "conv_lang", "old_pin_slot", "displaced_conv_id"},
				[][]driver.Value{{"CONFLICT", "fr", nil, int64(4001)}},
			), nil
		}, nil)

		req2 := httptest.NewRequest(http.MethodPost, "/admin/communities/pin", bytes.NewBufferString(`{"conversation_id":4002,"slot":1,"force_replace":false}`))
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusConflict, w2.Code)
	})
}
