package health_models

import (
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// NodeIdentity identifie l'instance courante (datacenter, rôle serveur, Node ID Snowflake).
type NodeIdentity struct {
	Datacenter string `json:"datacenter,omitempty"`
	Role       string `json:"role,omitempty"`
	NodeID     int64  `json:"node_id,omitempty"`
}

// Validate vérifie la cohérence des champs d'identité du nœud.
func (n *NodeIdentity) Validate() error {
	if n == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identité du nœud est vide.", nil)
	}
	if n.NodeID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de nœud ne peut pas être négatif.", nil)
	}
	return nil
}

// LivenessOutput représente la réponse JSON de la sonde /healthz.
type LivenessOutput struct {
	Status     string `json:"status"`
	Role       string `json:"role,omitempty"`
	Datacenter string `json:"datacenter,omitempty"`
	NodeID     int64  `json:"node_id,omitempty"`
}

// Validate vérifie que la réponse de vivacité est cohérente.
func (o *LivenessOutput) Validate() error {
	if o == nil || strings.TrimSpace(o.Status) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le statut de vivacité est requis.", nil)
	}
	return nil
}

// ReadinessOutput représente la réponse JSON de la sonde /readyz.
type ReadinessOutput struct {
	Status     string            `json:"status"`
	Role       string            `json:"role,omitempty"`
	Datacenter string            `json:"datacenter,omitempty"`
	NodeID     int64             `json:"node_id,omitempty"`
	Checks     map[string]string `json:"checks,omitempty"`
}

// Validate vérifie que la réponse de disponibilité est cohérente.
func (o *ReadinessOutput) Validate() error {
	if o == nil || strings.TrimSpace(o.Status) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le statut de disponibilité est requis.", nil)
	}
	return nil
}
