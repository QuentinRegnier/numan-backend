package moderation_handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminDelegationSwitchHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)

	r := gin.New()
	r.GET("/admin/moderation/delegation-switch", GetDelegationSwitchHandler)
	r.POST("/admin/moderation/delegation-switch", func(c *gin.Context) {
		c.Set("userID", int64(4001))
		c.Set("user_grade", variables.UserGradeAdmin)
		SetDelegationSwitchHandler(c)
	})

	// GET default state (false)
	reqGet := httptest.NewRequest(http.MethodGet, "/admin/moderation/delegation-switch", nil)
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, reqGet)
	assert.Equal(t, http.StatusOK, wGet.Code)

	var resGet moderation_models.AdminDelegationSwitchOutput
	require.NoError(t, json.Unmarshal(wGet.Body.Bytes(), &resGet))
	assert.False(t, resGet.Enabled)

	// POST toggle to true
	body, _ := json.Marshal(moderation_models.AdminDelegationSwitchInput{Enabled: true})
	reqPost := httptest.NewRequest(http.MethodPost, "/admin/moderation/delegation-switch", bytes.NewReader(body))
	reqPost.Header.Set("Content-Type", "application/json")
	wPost := httptest.NewRecorder()
	r.ServeHTTP(wPost, reqPost)
	assert.Equal(t, http.StatusOK, wPost.Code)

	var resPost moderation_models.AdminDelegationSwitchOutput
	require.NoError(t, json.Unmarshal(wPost.Body.Bytes(), &resPost))
	assert.True(t, resPost.Enabled)
	assert.True(t, variables.ModerationAdminDelegationEnabled)
}
