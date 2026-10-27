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

func TestAdminHardBanHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)

	r := gin.New()
	r.POST("/admin/moderation/hard-ban", func(c *gin.Context) {
		c.Set("userID", int64(4001))
		c.Set("user_grade", variables.UserGradeAdmin)
		AdminHardBanHandler(c)
	})

	body, _ := json.Marshal(moderation_models.AdminHardBanInput{
		TargetUserID:    7010,
		Reason:          "Violation extrême des CGU",
		CGUFeedbackNote: "Compte banni définitivement",
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/moderation/hard-ban", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var res moderation_models.AdminHardBanOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.True(t, res.Banned)
	assert.Equal(t, variables.TrustScoreHardBan, res.TrustScore)
	assert.Equal(t, int64(7010), res.TargetUserID)
}
