package admin_handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPauseSchedulerHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	scheduler_service.SetDefault(nil)

	newRouter := func(userID int64, grade int) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			if userID > 0 {
				c.Set("userID", userID)
			}
			c.Set("user_grade", grade)
			c.Next()
		})
		r.POST("/admin/scheduler/pause", PauseSchedulerHandler)
		return r
	}

	t.Run("400 when query parameters are passed", func(t *testing.T) {
		w := httptest.NewRecorder()
		newRouter(1, variables.UserGradeAdmin).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/scheduler/pause?ttl_minutes=15", nil))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400 when ttl_minutes exceeds 24h", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/scheduler/pause", bytes.NewBufferString(`{"ttl_minutes":9999}`))
		req.Header.Set("Content-Type", "application/json")
		newRouter(1, variables.UserGradeAdmin).ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("200 OK pauses scheduler", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/scheduler/pause", bytes.NewBufferString(`{"ttl_minutes":45,"reason":"schema_upgrade"}`))
		req.Header.Set("Content-Type", "application/json")
		newRouter(1, variables.UserGradeAdmin).ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var out admin_models.SchedulerActionOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		assert.Equal(t, "paused", out.Status)
		assert.Equal(t, 45, out.TTLMinutes)
		assert.Equal(t, "schema_upgrade", out.Reason)
	})
}
