package admin_handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/api/handlers/admin_handlers"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/service/cluster_service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetClusterTopologyHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = test.SetupTestRedis(t)
	t.Setenv("DATACENTER_ID", "fr-par")

	stop := cluster_service.StartNodeHeartbeat(context.Background(), cluster_service.NodeHeartbeatConfig{
		Datacenter:        "fr-par",
		Role:              "api_node",
		NodeID:            1,
		Hostname:          "api-1",
		HeartbeatInterval: time.Second,
		NodeTTL:           10 * time.Second,
	}, nil)
	defer stop()

	r := gin.New()
	r.GET("/admin/cluster/topology", admin_handlers.GetClusterTopologyHandler)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/cluster/topology", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	var resp cluster_service.ClusterTopologyResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "fr-par", resp.Datacenter)
	assert.Equal(t, 1, resp.TotalNodes)
	assert.Equal(t, int64(1), resp.Nodes[0].NodeID)
}
