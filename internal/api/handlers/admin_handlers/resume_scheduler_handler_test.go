package admin_handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResumeSchedulerHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	scheduler_service.SetDefault(nil)
	require.NoError(t, scheduler_service.PauseGlobal(context.Background(), 10*time.Minute, "maintenance"))

	newRouter := func(userID int64, grade int) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			if userID > 0 {
				c.Set("userID", userID)
			}
			c.Set("user_grade", grade)
			c.Next()
		})
		r.POST("/admin/scheduler/resume", ResumeSchedulerHandler)
		return r
	}

	t.Run("403 when non-admin", func(t *testing.T) {
		w := httptest.NewRecorder()
		newRouter(10, variables.UserGradeUser).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/scheduler/resume", nil))
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("400 when query parameters are passed", func(t *testing.T) {
		w := httptest.NewRecorder()
		newRouter(1, variables.UserGradeAdmin).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/scheduler/resume?force=true", nil))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("200 OK resumes scheduler", func(t *testing.T) {
		w := httptest.NewRecorder()
		newRouter(1, variables.UserGradeAdmin).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/scheduler/resume", nil))
		require.Equal(t, http.StatusOK, w.Code)

		var out admin_models.SchedulerActionOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		assert.Equal(t, "resumed", out.Status)
	})
}
