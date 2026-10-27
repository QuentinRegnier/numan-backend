package moderation_handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModBatchLikeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	r := gin.New()
	r.POST("/admin/moderation/batch-like", func(c *gin.Context) {
		c.Set("userID", int64(3001))
		c.Set("user_grade", variables.UserGradeModerator)
		ModBatchLikeHandler(c)
	})

	body, _ := json.Marshal(moderation_models.ModBatchLikeInput{
		AuthorUserIDs: []int64{101, 102, 103},
		Reason:        "Validation de publications remarquables",
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/moderation/batch-like", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var res moderation_models.ModBatchLikeOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.Equal(t, 3, res.UsersCount)
	assert.Greater(t, res.EventID, int64(0))
}
