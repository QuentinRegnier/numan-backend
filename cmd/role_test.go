package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveServerRole(t *testing.T) {
	t.Setenv("SERVER_ROLE", "")
	assert.Equal(t, RoleMonolith, ResolveServerRole())
	assert.True(t, RoleMonolith.RunsREST())
	assert.True(t, RoleMonolith.RunsWebSocket())
	assert.True(t, RoleMonolith.RunsAPI())
	assert.True(t, RoleMonolith.RunsWorkers())
	assert.True(t, RoleMonolith.NeedsCuckooFilter())
	assert.True(t, RoleMonolith.NeedsStartupSeeding())

	t.Setenv("SERVER_ROLE", "api_node")
	roleAPI := ResolveServerRole()
	assert.Equal(t, RoleAPINode, roleAPI)
	assert.True(t, roleAPI.RunsREST())
	assert.False(t, roleAPI.RunsWebSocket())
	assert.True(t, roleAPI.RunsAPI())
	assert.False(t, roleAPI.RunsWorkers())
	assert.True(t, roleAPI.NeedsCuckooFilter())
	assert.True(t, roleAPI.NeedsStartupSeeding())

	t.Setenv("SERVER_ROLE", "ws_node")
	roleWS := ResolveServerRole()
	assert.Equal(t, RoleWSNode, roleWS)
	assert.False(t, roleWS.RunsREST())
	assert.True(t, roleWS.RunsWebSocket())
	assert.True(t, roleWS.RunsAPI())
	assert.False(t, roleWS.RunsWorkers())
	assert.False(t, roleWS.NeedsCuckooFilter())
	assert.False(t, roleWS.NeedsStartupSeeding())

	t.Setenv("SERVER_ROLE", " WORKER_NODE ")
	roleWorker := ResolveServerRole()
	assert.Equal(t, RoleWorkerNode, roleWorker)
	assert.False(t, roleWorker.RunsREST())
	assert.False(t, roleWorker.RunsWebSocket())
	assert.False(t, roleWorker.RunsAPI())
	assert.True(t, roleWorker.RunsWorkers())
	assert.True(t, roleWorker.NeedsCuckooFilter())
	assert.True(t, roleWorker.NeedsStartupSeeding())
}

func TestResolveDatacenterID(t *testing.T) {
	t.Setenv("DATACENTER_ID", "")
	assert.Equal(t, "default", ResolveDatacenterID())

	t.Setenv("DATACENTER_ID", " DE-FRA ")
	assert.Equal(t, "de-fra", ResolveDatacenterID())
}
