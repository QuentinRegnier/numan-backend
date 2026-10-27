package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrustScoreMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	// 1. Utilisateur Hard Banned (Score = 0.00) -> 403 USER_BANNED
	require.NoError(t, redis.UsersLite.SetObject(ctx, 10, lite_models.UserLiteRequest{ID: 10, Username: "banned_user", TrustScore: 0.00}))
	// 2. Utilisateur en Quarantaine (Score = 0.20) -> 200 OK + force_quarantine = true
	require.NoError(t, redis.UsersLite.SetObject(ctx, 20, lite_models.UserLiteRequest{ID: 20, Username: "new_user", TrustScore: 0.20}))
	// 3. Utilisateur Standard (Score = 0.65) -> 200 OK + force_quarantine = false
	require.NoError(t, redis.UsersLite.SetObject(ctx, 30, lite_models.UserLiteRequest{ID: 30, Username: "std_user", TrustScore: 0.65}))

	buildRouter := func(uid int64) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set("userID", uid)
			c.Next()
		})
		r.Use(TrustScoreMiddleware())
		r.POST("/upload", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"force_quarantine": c.GetBool(ContextKeyForceQuarantine),
				"trust_score":      c.GetFloat64(ContextKeyTrustScore),
			})
		})
		return r
	}

	t.Run("Hard Ban user (0.00) is blocked with 403", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/upload", nil)
		buildRouter(10).ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "USER_BANNED")
	})

	t.Run("Quarantine user (0.20 < 0.40) passes with force_quarantine=true", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/upload", nil)
		buildRouter(20).ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"force_quarantine":true`)
	})

	t.Run("Standard user (0.65 >= 0.40) passes with force_quarantine=false", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/upload", nil)
		buildRouter(30).ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"force_quarantine":false`)
	})
}
