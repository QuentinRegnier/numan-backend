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

func TestGetSchedulerJobsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)

	eng := scheduler_service.NewEngine("dc-eu-1", scheduler_service.DefaultRegimeConfig(), nil)
	scheduler_service.RegisterAllDefaultJobs(eng)
	scheduler_service.SetDefault(eng)
	t.Cleanup(func() { scheduler_service.SetDefault(nil) })

	newRouter := func(userID int64, grade int) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			if userID > 0 {
				c.Set("userID", userID)
			}
			c.Set("user_grade", grade)
			c.Next()
		})
		r.POST("/admin/scheduler/jobs", GetSchedulerJobsHandler)
		return r
	}

	t.Run("403 for non-admin", func(t *testing.T) {
		w := httptest.NewRecorder()
		newRouter(10, variables.UserGradeUser).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/scheduler/jobs", nil))
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("400 when query parameters are passed", func(t *testing.T) {
		w := httptest.NewRecorder()
		newRouter(1, variables.UserGradeAdmin).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/scheduler/jobs?datacenter=dc-eu-1", nil))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("200 OK returns 24 scheduled jobs", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/scheduler/jobs", bytes.NewBufferString(`{"datacenter":"dc-eu-1"}`))
		req.Header.Set("Content-Type", "application/json")
		newRouter(1, variables.UserGradeAdmin).ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var out admin_models.GetSchedulerJobsOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		assert.Equal(t, "dc-eu-1", out.Datacenter)
		assert.Len(t, out.Jobs, 24)
	})
}
