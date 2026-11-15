package report_handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateReportHandler_ValidationAndTargetTypeZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	router := gin.New()
	router.POST("/report", func(c *gin.Context) {
		c.Set("userID", int64(42))
		CreateReportHandler(c)
	})

	invalidPayloads := []struct {
		name string
		body string
	}{
		{"negative_target_id", `{"target_type":0,"target_ids":[-1],"category":1,"reason":"spam"}`},
		{"empty_target_ids", `{"target_type":0,"target_ids":[],"category":1}`},
		{"invalid_category", `{"target_type":0,"target_ids":[10],"category":999}`},
		{"invalid_target_type", `{"target_type":4,"target_ids":[10],"category":1}`},
	}

	for _, tc := range invalidPayloads {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/report", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "INVALID_PAYLOAD")
		})
	}

	keys, errKeys := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, errKeys)
	assert.Empty(t, keys, "Invalid report requests must not mutate Redis")

	// Regression check: target_type=0 (ReportTargetPost) must be accepted (previously blocked by binding:"required")
	t.Run("accepts_target_type_zero_for_post_report", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/report", bytes.NewBufferString(`{"target_type":0,"target_ids":[500],"category":1,"reason":"spam post"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}
