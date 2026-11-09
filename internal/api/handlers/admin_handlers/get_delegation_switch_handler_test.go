package admin_handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDelegationSwitchHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)

	r := gin.New()
	r.GET("/admin/moderation/delegation-switch", GetDelegationSwitchHandler)

	reqGet := httptest.NewRequest(http.MethodGet, "/admin/moderation/delegation-switch", nil)
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, reqGet)

	assert.Equal(t, http.StatusOK, wGet.Code)

	var resGet admin_models.AdminDelegationSwitchOutput
	require.NoError(t, json.Unmarshal(wGet.Body.Bytes(), &resGet))
	assert.False(t, resGet.Enabled)
}
