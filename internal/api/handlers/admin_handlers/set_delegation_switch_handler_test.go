package admin_handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetDelegationSwitchHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)

	r := gin.New()
	r.POST("/admin/moderation/delegation-switch", func(c *gin.Context) {
		c.Set("userID", int64(4001))
		c.Set("user_grade", variables.UserGradeAdmin)
		SetDelegationSwitchHandler(c)
	})

	body, _ := json.Marshal(admin_models.AdminDelegationSwitchInput{Enabled: true})
	reqPost := httptest.NewRequest(http.MethodPost, "/admin/moderation/delegation-switch", bytes.NewReader(body))
	reqPost.Header.Set("Content-Type", "application/json")

	wPost := httptest.NewRecorder()
	r.ServeHTTP(wPost, reqPost)

	assert.Equal(t, http.StatusOK, wPost.Code)

	var resPost admin_models.AdminDelegationSwitchOutput
	require.NoError(t, json.Unmarshal(wPost.Body.Bytes(), &resPost))
	assert.True(t, resPost.Enabled)
	assert.True(t, variables.ModerationAdminDelegationEnabled)
}
