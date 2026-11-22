package cluster_service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

const (
	defaultHeartbeatInterval = 10 * time.Second
	defaultNodeTTL           = 30 * time.Second
)

// NodeHeartbeatConfig identifie un nœud Go qui s'annonce auprès du registre de son datacenter.
type NodeHeartbeatConfig struct {
	Datacenter        string
	Role              string
	NodeID            int64
	Hostname          string
	PodName           string
	HeartbeatInterval time.Duration
	NodeTTL           time.Duration
}

// ClusterNodeInfo représente l'état temps réel d'un nœud découvert dans le datacenter.
type ClusterNodeInfo struct {
	NodeID        int64  `json:"node_id"`
	Role          string `json:"role"`
	Datacenter    string `json:"datacenter"`
	Hostname      string `json:"hostname"`
	PodName       string `json:"pod_name,omitempty"`
	Status        string `json:"status"`
	StartedAtMs   int64  `json:"started_at_ms"`
	LastSeenMs    int64  `json:"last_seen_ms"`
	WSUsers       int64  `json:"ws_users"`
	WSConnections int64  `json:"ws_connections"`
}

// ClusterTopologyResponse expose la topologie live d'un datacenter pour la CLI d'administration.
type ClusterTopologyResponse struct {
	Datacenter         string            `json:"datacenter"`
	KnownDatacenters   []string          `json:"known_datacenters"`
	TotalNodes         int               `json:"total_nodes"`
	RoleCounts         map[string]int    `json:"role_counts"`
	TotalWSUsers       int64             `json:"total_ws_users"`
	TotalWSConnections int64             `json:"total_ws_connections"`
	RedisQueueDepth    int64             `json:"redis_queue_depth"`
	CollectedAtMs      int64             `json:"collected_at_ms"`
	Nodes              []ClusterNodeInfo `json:"nodes"`
}

func normalizeDatacenter(dc string) string {
	dc = strings.ToLower(strings.TrimSpace(dc))
	if dc == "" {
		dc = strings.ToLower(strings.TrimSpace(os.Getenv("DATACENTER_ID")))
	}
	if dc == "" {
		return "default"
	}
	return dc
}

func memberKey(role string, nodeID int64) string {
	return fmt.Sprintf("%s:%d", strings.ToLower(strings.TrimSpace(role)), nodeID)
}

// StartNodeHeartbeat publie immédiatement la présence du nœud dans Redis L1,
// la renouvelle périodiquement, et retourne une fonction stop() qui le désenregistre proprement.
func StartNodeHeartbeat(
	parentCtx context.Context,
	cfg NodeHeartbeatConfig,
	wsStatsFn func() (activeUsers int64, activeConnections int64),
) (stop func()) {
	dc := normalizeDatacenter(cfg.Datacenter)
	role := strings.ToLower(strings.TrimSpace(cfg.Role))
	if role == "" {
		role = "monolith"
	}
	hostname := strings.TrimSpace(cfg.Hostname)
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	podName := strings.TrimSpace(cfg.PodName)
	if podName == "" {
		podName = strings.TrimSpace(os.Getenv("POD_NAME"))
	}
	interval := cfg.HeartbeatInterval
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	ttl := cfg.NodeTTL
	if ttl <= 0 {
		ttl = defaultNodeTTL
	}

	startedAt := domain.NowMillis()
	memberID := memberKey(role, cfg.NodeID)

	publish := func(ctx context.Context, status string) {
		var users, conns int64
		if wsStatsFn != nil {
			users, conns = wsStatsFn()
		}
		info := ClusterNodeInfo{
			NodeID:        cfg.NodeID,
			Role:          role,
			Datacenter:    dc,
			Hostname:      hostname,
			PodName:       podName,
			Status:        status,
			StartedAtMs:   startedAt,
			LastSeenMs:    domain.NowMillis(),
			WSUsers:       users,
			WSConnections: conns,
		}
		reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := repoRedis.RegisterClusterNode(reqCtx, dc, memberID, info, ttl); err != nil {
			numan_log.Warn(ctx).Err(err).Str("datacenter", dc).Str("role", role).Int64("node_id", cfg.NodeID).
				Msg("Échec de l'enregistrement heartbeat cluster Redis")
		}
	}

	// Publication initiale synchrone pour être visible dès le démarrage
	publish(parentCtx, "ready")

	ctx, cancel := context.WithCancel(parentCtx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				publish(ctx, "ready")
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
			unregCtx, unregCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer unregCancel()
			if err := repoRedis.UnregisterClusterNode(unregCtx, dc, memberID); err != nil {
				numan_log.Warn(unregCtx).Err(err).Str("datacenter", dc).Str("member", memberID).
					Msg("Échec du désenregistrement cluster Redis lors du shutdown")
			}
		})
	}
}

// GetDatacenterTopology lit l'ensemble des nœuds vivants du datacenter cible depuis Redis L1.
func GetDatacenterTopology(ctx context.Context, datacenter string) (*ClusterTopologyResponse, *numan_error.AppError) {
	dc := normalizeDatacenter(datacenter)

	rawNodes, err := repoRedis.ListClusterNodePayloads(ctx, dc)
	if err != nil {
		return nil, numan_error.NewInternal(err)
	}

	knownDCs, err := repoRedis.ListRegisteredDatacenters(ctx)
	if err != nil {
		knownDCs = []string{dc}
	}
	sort.Strings(knownDCs)

	nodes := make([]ClusterNodeInfo, 0, len(rawNodes))
	roleCounts := make(map[string]int)
	var totalUsers, totalConns int64

	for _, raw := range rawNodes {
		var node ClusterNodeInfo
		if err := json.Unmarshal(raw, &node); err != nil {
			continue
		}
		nodes = append(nodes, node)
		roleCounts[node.Role]++
		totalUsers += node.WSUsers
		totalConns += node.WSConnections
	}

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Role != nodes[j].Role {
			return nodes[i].Role < nodes[j].Role
		}
		return nodes[i].NodeID < nodes[j].NodeID
	})

	queueDepth, _ := repoRedis.TotalQueueDepth(ctx)

	return &ClusterTopologyResponse{
		Datacenter:         dc,
		KnownDatacenters:   knownDCs,
		TotalNodes:         len(nodes),
		RoleCounts:         roleCounts,
		TotalWSUsers:       totalUsers,
		TotalWSConnections: totalConns,
		RedisQueueDepth:    queueDepth,
		CollectedAtMs:      domain.NowMillis(),
		Nodes:              nodes,
	}, nil
}
