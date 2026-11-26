package cluster_service_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cluster_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterService_HeartbeatDiscoveryAndGracefulUnregister(t *testing.T) {
	srv := test.SetupTestRedis(t)
	ctx := context.Background()

	// Enregistre un nœud API et un nœud WebSocket dans le datacenter de-fra, et un autre dans fr-par
	stopAPI := cluster_service.StartNodeHeartbeat(ctx, cluster_service.NodeHeartbeatConfig{
		Datacenter:        "de-fra",
		Role:              "api_node",
		NodeID:            129,
		Hostname:          "api-129",
		HeartbeatInterval: 200 * time.Millisecond,
		NodeTTL:           5 * time.Second,
	}, nil)
	defer stopAPI()

	stopWS := cluster_service.StartNodeHeartbeat(ctx, cluster_service.NodeHeartbeatConfig{
		Datacenter:        "de-fra",
		Role:              "ws_node",
		NodeID:            180,
		Hostname:          "websocket-180",
		HeartbeatInterval: 200 * time.Millisecond,
		NodeTTL:           5 * time.Second,
	}, func() (int64, int64) { return 15, 22 })
	defer stopWS()

	stopFR := cluster_service.StartNodeHeartbeat(ctx, cluster_service.NodeHeartbeatConfig{
		Datacenter:        "fr-par",
		Role:              "monolith",
		NodeID:            1,
		Hostname:          "api-1",
		HeartbeatInterval: 5 * time.Second,
		NodeTTL:           5 * time.Second,
	}, nil)
	defer stopFR()

	// Ajoute 2 événements en attente dans la file Redis Write-Behind
	require.NoError(t, repoRedis.EnqueueDB(ctx, 11, 0, repoRedis.EntityPost, repoRedis.ActionCreate, map[string]any{"id": 11}, repoRedis.TargetAll))
	require.NoError(t, repoRedis.EnqueueDB(ctx, 22, 0, repoRedis.EntityLike, repoRedis.ActionCreate, map[string]any{"id": 22}, repoRedis.TargetPostgres))

	topo, appErr := cluster_service.GetDatacenterTopology(ctx, "de-fra")
	require.Nil(t, appErr)
	assert.Equal(t, "de-fra", topo.Datacenter)
	assert.Equal(t, []string{"de-fra", "fr-par"}, topo.KnownDatacenters)
	assert.Equal(t, 2, topo.TotalNodes)
	assert.Equal(t, 1, topo.RoleCounts["api_node"])
	assert.Equal(t, 1, topo.RoleCounts["ws_node"])
	assert.Equal(t, int64(15), topo.TotalWSUsers)
	assert.Equal(t, int64(22), topo.TotalWSConnections)
	assert.Equal(t, int64(2), topo.RedisQueueDepth)

	// Graceful shutdown du nœud WS -> retrait immédiat du registre
	stopWS()

	topoAfterStop, appErr := cluster_service.GetDatacenterTopology(ctx, "de-fra")
	require.Nil(t, appErr)
	assert.Equal(t, 1, topoAfterStop.TotalNodes)
	assert.Equal(t, "api_node", topoAfterStop.Nodes[0].Role)
	assert.Equal(t, int64(0), topoAfterStop.TotalWSConnections)

	// Simulation d'expiration TTL après crash brutal (la clé du nœud expire alors que le SET conserve encore l'entrée)
	srv.DeleteKey("cluster:node:fr-par:monolith:1")
	topoExpired, appErr := cluster_service.GetDatacenterTopology(ctx, "fr-par")
	require.Nil(t, appErr)
	assert.Equal(t, 0, topoExpired.TotalNodes)

	// Vérifie que le membre expiré a bien été purgé du SET du datacenter
	assert.Empty(t, srv.SetMembers("cluster:dc:fr-par:nodes"))
}
