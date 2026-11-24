package health_service_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/service/health_service"
	"github.com/stretchr/testify/assert"
)

func TestCheckLiveness(t *testing.T) {
	t.Cleanup(func() {
		health_service.SetNodeIdentity("", "", 0)
	})

	out := health_service.CheckLiveness(context.Background())
	assert.Equal(t, "alive", out.Status)
	assert.Empty(t, out.Role)

	health_service.SetNodeIdentity("dc-eu-1", "scheduler_node", 3)
	out = health_service.CheckLiveness(context.Background())
	assert.Equal(t, "alive", out.Status)
	assert.Equal(t, "scheduler_node", out.Role)
	assert.Equal(t, "dc-eu-1", out.Datacenter)
	assert.Equal(t, int64(3), out.NodeID)
}
