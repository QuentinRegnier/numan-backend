package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/api/middleware"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimiter_Exceeded_PublishesSpamDetectedAuto(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	gin.SetMode(gin.TestMode)

	callerID := int64(987654)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", callerID)
		c.Next()
	})
	router.Use(middleware.RateLimiter())
	router.GET("/test-ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	// Exécute 51 requêtes pour dépasser rateLimitMaxRequests (50)
	var resp *httptest.ResponseRecorder
	for i := 0; i < 51; i++ {
		req, _ := http.NewRequest("GET", "/test-ping", nil)
		req.RemoteAddr = "192.168.1.100:1234"
		resp = httptest.NewRecorder()
		router.ServeHTTP(resp, req)
	}

	assert.Equal(t, http.StatusTooManyRequests, resp.Code)

	// Vérifie que l'événement anti-flood / spam auto (-0.05) a été publié
	rawEvt, errPop := redis.WorkerQueue.LPop(context.Background(), variables.WorkerQueueTrustScore)
	require.NoError(t, errPop)
	assert.Contains(t, rawEvt, variables.TrustTriggerSpamDetectedAuto)
	assert.Contains(t, rawEvt, "987654")
}
