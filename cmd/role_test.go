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
	assert.True(t, RoleMonolith.RunsWriteBehind())
	assert.True(t, RoleMonolith.RunsScheduler())
	assert.True(t, RoleMonolith.NeedsCuckooFilter())
	assert.True(t, RoleMonolith.NeedsStartupSeeding())

	t.Setenv("SERVER_ROLE", "api_node")
	roleAPI := ResolveServerRole()
	assert.Equal(t, RoleAPINode, roleAPI)
	assert.True(t, roleAPI.RunsREST())
	assert.False(t, roleAPI.RunsWebSocket())
	assert.True(t, roleAPI.RunsAPI())
	assert.False(t, roleAPI.RunsWorkers())
	assert.False(t, roleAPI.RunsWriteBehind())
	assert.False(t, roleAPI.RunsScheduler())
	assert.True(t, roleAPI.NeedsCuckooFilter())
	assert.True(t, roleAPI.NeedsStartupSeeding())

	t.Setenv("SERVER_ROLE", "ws_node")
	roleWS := ResolveServerRole()
	assert.Equal(t, RoleWSNode, roleWS)
	assert.False(t, roleWS.RunsREST())
	assert.True(t, roleWS.RunsWebSocket())
	assert.True(t, roleWS.RunsAPI())
	assert.False(t, roleWS.RunsWorkers())
	assert.False(t, roleWS.RunsWriteBehind())
	assert.False(t, roleWS.RunsScheduler())
	assert.False(t, roleWS.NeedsCuckooFilter())
	assert.False(t, roleWS.NeedsStartupSeeding())

	t.Setenv("SERVER_ROLE", " WORKER_NODE ")
	roleWorker := ResolveServerRole()
	assert.Equal(t, RoleWorkerNode, roleWorker)
	assert.False(t, roleWorker.RunsREST())
	assert.False(t, roleWorker.RunsWebSocket())
	assert.False(t, roleWorker.RunsAPI())
	assert.True(t, roleWorker.RunsWorkers())
	assert.True(t, roleWorker.RunsWriteBehind())
	assert.False(t, roleWorker.RunsScheduler())
	assert.True(t, roleWorker.NeedsCuckooFilter())
	assert.True(t, roleWorker.NeedsStartupSeeding())

	t.Setenv("SERVER_ROLE", " SCHEDULER_NODE ")
	roleSched := ResolveServerRole()
	assert.Equal(t, RoleSchedulerNode, roleSched)
	assert.False(t, roleSched.RunsREST())
	assert.False(t, roleSched.RunsWebSocket())
	assert.False(t, roleSched.RunsAPI())
	assert.False(t, roleSched.RunsWorkers())
	assert.False(t, roleSched.RunsWriteBehind())
	assert.True(t, roleSched.RunsScheduler())
	assert.True(t, roleSched.NeedsCuckooFilter())
	assert.False(t, roleSched.NeedsStartupSeeding())
}

func TestResolveDatacenterID(t *testing.T) {
	t.Setenv("DATACENTER_ID", "")
	assert.Equal(t, "default", ResolveDatacenterID())

	t.Setenv("DATACENTER_ID", " DE-FRA ")
	assert.Equal(t, "de-fra", ResolveDatacenterID())
}
