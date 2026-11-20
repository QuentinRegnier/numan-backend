package article_handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsCallerAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)

	t.Run("admin detected from user_grade in gin context", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/articles/get", nil)
		c.Set("user_grade", variables.UserGradeAdmin)

		assert.True(t, isCallerAdmin(c, 1001))
	})

	t.Run("fallback member role admin in gin context returns true", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/articles/get", nil)
		c.Set("user_grade", variables.MemberRoleAdmin)

		assert.True(t, isCallerAdmin(c, 1002))
	})

	t.Run("regular user in gin context returns false", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/articles/get", nil)
		c.Set("user_grade", variables.UserGradeUser)

		assert.False(t, isCallerAdmin(c, 1003))
	})

	t.Run("fallback to UsersLite cache when user_grade missing from context", func(t *testing.T) {
		require.NoError(t, redis.UsersLite.SetObject(context.Background(), 9501, lite_models.UserLiteRequest{
			ID:       9501,
			Username: "admin_l1",
			Grade:    variables.UserGradeAdmin,
		}))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/articles/get", nil)

		assert.True(t, isCallerAdmin(c, 9501))
	})

	t.Run("missing user_grade and non-positive callerID returns false", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/articles/get", nil)

		assert.False(t, isCallerAdmin(c, 0))
	})
}

func TestRejectQueryParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("accepts clean URL", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/articles/list", nil)

		require.NoError(t, rejectQueryParameters(c))
	})

	t.Run("rejects query parameters", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/articles/list?limit=10", nil)

		err := rejectQueryParameters(c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "query")
	})
}
