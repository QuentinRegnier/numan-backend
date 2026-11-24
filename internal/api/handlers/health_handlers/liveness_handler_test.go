package health_handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/health_models"
	"github.com/QuentinRegnier/numan-backend/internal/service/health_service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLivenessHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { health_service.SetNodeIdentity("", "", 0) })

	health_service.SetNodeIdentity("dc-eu-1", "api_node", 9)

	r := gin.New()
	r.GET("/healthz", LivenessHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var out health_models.LivenessOutput
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "alive", out.Status)
	assert.Equal(t, "api_node", out.Role)
	assert.Equal(t, "dc-eu-1", out.Datacenter)
	assert.Equal(t, int64(9), out.NodeID)
}
