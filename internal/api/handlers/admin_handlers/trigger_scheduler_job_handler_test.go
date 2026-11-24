package admin_handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTriggerSchedulerJobHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)

	eng := scheduler_service.NewEngine("dc-eu-1", scheduler_service.DefaultRegimeConfig(), nil)
	eng.Register(scheduler_service.JobDefinition{
		Name:      "test_manual_job",
		Category:  "hygiene",
		MinRegime: scheduler_models.RegimeDeepIdle,
		LockScope: scheduler_models.LockScopeLocalDC,
		Fn: func(_ context.Context, cp scheduler_models.JobCheckpoint, _ bool) (scheduler_models.JobCheckpoint, bool, error) {
			return cp, true, nil
		},
	})
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
		r.POST("/admin/scheduler/jobs/trigger", TriggerSchedulerJobHandler)
		return r
	}

	t.Run("400 when query parameters are passed", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/scheduler/jobs/trigger?force=true", bytes.NewBufferString(`{"job_name":"test_manual_job"}`))
		req.Header.Set("Content-Type", "application/json")
		newRouter(1, variables.UserGradeAdmin).ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400 when body missing or invalid job_name", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/scheduler/jobs/trigger", bytes.NewBufferString(`{"job_name":"INVALID!"}`))
		req.Header.Set("Content-Type", "application/json")
		newRouter(1, variables.UserGradeAdmin).ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("404 when job does not exist", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/scheduler/jobs/trigger", bytes.NewBufferString(`{"job_name":"nonexistent_job"}`))
		req.Header.Set("Content-Type", "application/json")
		newRouter(1, variables.UserGradeAdmin).ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("200 OK when executed locally", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/scheduler/jobs/trigger", bytes.NewBufferString(`{"job_name":"test_manual_job","force":true}`))
		req.Header.Set("Content-Type", "application/json")
		newRouter(1, variables.UserGradeAdmin).ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var out admin_models.SchedulerActionOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		assert.Equal(t, "executed", out.Status)
		assert.False(t, out.Queued)
	})

	t.Run("202 Accepted when queued for remote scheduler_node", func(t *testing.T) {
		scheduler_service.SetDefault(nil)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/scheduler/jobs/trigger", bytes.NewBufferString(`{"job_name":"test_manual_job","force":true}`))
		req.Header.Set("Content-Type", "application/json")
		newRouter(1, variables.UserGradeAdmin).ServeHTTP(w, req)
		require.Equal(t, http.StatusAccepted, w.Code)

		var out admin_models.SchedulerActionOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		assert.Equal(t, "queued_remote_trigger", out.Status)
		assert.True(t, out.Queued)
	})
}
