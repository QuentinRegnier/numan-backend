package moderation_handlers

import (
	"bytes"
	"context"
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModerationHandlers_ValidationAndDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", int64(3001))
		c.Set("user_grade", variables.UserGradeModerator)
		c.Next()
	})
	r.POST("/admin/moderation/batch-like", ModBatchLikeHandler)
	r.POST("/admin/moderation/quarantine/review", ReviewQuarantineHandler)
	r.POST("/admin/moderation/queue", GetModerationQueueHandler)

	// 1. ModBatchLikeHandler rejects negative author ID and leaves Redis clean
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/moderation/batch-like", bytes.NewBufferString(`{"author_user_ids":[-1],"reason":"bad"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "INVALID_PAYLOAD")

	// 2. ReviewQuarantineHandler rejects negative media_id / post_id
	for _, body := range []string{
		`{"media_id":-1,"decision":"approve"}`,
		`{"media_id":10,"post_id":-1,"decision":"approve"}`,
		`{"media_id":10,"decision":"bad"}`,
	} {
		w = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/admin/moderation/quarantine/review", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "INVALID_PAYLOAD")
	}

	// 3. GetModerationQueueHandler rejects invalid queue_type / limit / offset
	for _, body := range []string{
		`{"queue_type":"invalid"}`,
		`{"limit":101}`,
		`{"offset":-1}`,
	} {
		w = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/admin/moderation/queue", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "INVALID_PAYLOAD")
	}

	keys, errKeys := redis.ScanKeys(ctx, "*", 200)
	require.NoError(t, errKeys)
	assert.Empty(t, keys, "Invalid moderation handler calls must not mutate Redis")

	// 4. GetModerationQueueHandler accepts empty body and applies default limit=50 in service
	require.NoError(t, redis.UsersLite.SetObject(ctx, 3001, lite_models.UserLiteRequest{
		ID:       3001,
		Username: "mod_3001",
		Grade:    variables.UserGradeModerator,
	}))
	var capturedLimit int64
	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		if len(args) > 0 {
			if v, ok := args[0].Value.(int64); ok && capturedLimit == 0 {
				capturedLimit = v
			}
		}
		return test.NewMockRows([]string{}, nil), nil
	}, nil)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/moderation/queue", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, int64(50), capturedLimit)
}
