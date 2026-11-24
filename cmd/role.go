package main

import (
	"os"
	"strings"
)

// ServerRole décrit le rôle opérationnel de cette instance ./numan.
type ServerRole string

const (
	RoleMonolith      ServerRole = "monolith"
	RoleAPINode       ServerRole = "api_node"
	RoleWSNode        ServerRole = "ws_node"
	RoleWorkerNode    ServerRole = "worker_node"
	RoleSchedulerNode ServerRole = "scheduler_node"
)

// ResolveServerRole lit SERVER_ROLE et normalise le mode d'exécution.
// Par défaut (vide ou inconnu), retourne RoleMonolith pour préserver la rétrocompatibilité dev.
func ResolveServerRole() ServerRole {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SERVER_ROLE"))) {
	case string(RoleAPINode):
		return RoleAPINode
	case string(RoleWSNode):
		return RoleWSNode
	case string(RoleWorkerNode):
		return RoleWorkerNode
	case string(RoleSchedulerNode):
		return RoleSchedulerNode
	default:
		return RoleMonolith
	}
}

// ResolveDatacenterID retourne l'identifiant logique du datacenter (ex. fr-par, de-fra).
func ResolveDatacenterID() string {
	dc := strings.ToLower(strings.TrimSpace(os.Getenv("DATACENTER_ID")))
	if dc == "" {
		return "default"
	}
	return dc
}

// RunsREST indique si ce nœud expose les routes HTTP REST (et Swagger).
func (r ServerRole) RunsREST() bool {
	return r == RoleMonolith || r == RoleAPINode
}

// RunsWebSocket indique si ce nœud initialise le Hub WebSocket et expose /ws.
func (r ServerRole) RunsWebSocket() bool {
	return r == RoleMonolith || r == RoleWSNode
}

// RunsAPI indique si ce nœud sert du trafic client (REST ou WebSocket).
func (r ServerRole) RunsAPI() bool {
	return r.RunsREST() || r.RunsWebSocket()
}

// RunsWriteBehind indique si ce nœud exécute le pool Write-Behind 64 shards + consommateurs temps réel (Push, Email, Trust Queue, Moderation Expiration).
func (r ServerRole) RunsWriteBehind() bool {
	return r == RoleMonolith || r == RoleWorkerNode
}

// RunsWorkers est un alias rétrocompatible de RunsWriteBehind.
func (r ServerRole) RunsWorkers() bool {
	return r.RunsWriteBehind()
}

// RunsScheduler indique si ce nœud exécute l'agrégateur télémétrie DC et l'Activity-Aware Job Scheduler.
// En mode monolith, le scheduler tourne embarqué (0 pod supplémentaire).
// En production distribuée/HA, il tourne dans son pod dédié (SERVER_ROLE=scheduler_node).
func (r ServerRole) RunsScheduler() bool {
	return r == RoleMonolith || r == RoleSchedulerNode
}

// NeedsCuckooFilter indique si ce nœud a besoin du Cuckoo Filter en RAM (REST, Worker ou Scheduler pour l'audit/rebuild).
func (r ServerRole) NeedsCuckooFilter() bool {
	return r.RunsREST() || r.RunsWriteBehind() || r.RunsScheduler()
}

// NeedsStartupSeeding indique si ce nœud participe au Smart Seeding initial de Redis / index Mongo.
func (r ServerRole) NeedsStartupSeeding() bool {
	return r.RunsREST() || r.RunsWriteBehind()
}
