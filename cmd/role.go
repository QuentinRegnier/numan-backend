package main

import (
	"os"
	"strings"
)

// ServerRole décrit le rôle opérationnel de cette instance ./numan.
type ServerRole string

const (
	RoleMonolith   ServerRole = "monolith"
	RoleAPINode    ServerRole = "api_node"
	RoleWSNode     ServerRole = "ws_node"
	RoleWorkerNode ServerRole = "worker_node"
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

// RunsWorkers indique si ce nœud exécute les 64 workers Write-Behind et les crons périodiques.
func (r ServerRole) RunsWorkers() bool {
	return r == RoleMonolith || r == RoleWorkerNode
}

// NeedsCuckooFilter indique si ce nœud a besoin du Cuckoo Filter en RAM (feeds REST & warmup worker).
func (r ServerRole) NeedsCuckooFilter() bool {
	return r.RunsREST() || r.RunsWorkers()
}

// NeedsStartupSeeding indique si ce nœud participe au Smart Seeding initial de Redis / index Mongo.
func (r ServerRole) NeedsStartupSeeding() bool {
	return r.RunsREST() || r.RunsWorkers()
}
