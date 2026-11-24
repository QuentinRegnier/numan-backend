package health_service_test

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/service/health_service"
	"github.com/stretchr/testify/assert"
)

func TestNodeIdentityAndShutdownState(t *testing.T) {
	t.Cleanup(func() {
		health_service.SetNodeIdentity("", "", 0)
		health_service.SetShuttingDown(false)
	})

	health_service.SetNodeIdentity("dc-eu-1", "api_node", 7)
	id := health_service.CurrentNodeIdentity()
	assert.Equal(t, "dc-eu-1", id.Datacenter)
	assert.Equal(t, "api_node", id.Role)
	assert.Equal(t, int64(7), id.NodeID)

	assert.False(t, health_service.IsShuttingDown())
	health_service.SetShuttingDown(true)
	assert.True(t, health_service.IsShuttingDown())
}
