package health_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/health_models"
)

// CheckLiveness retourne l'état de vivacité du processus Go (sans dépendre des BDD externes).
func CheckLiveness(_ context.Context) health_models.LivenessOutput {
	id := CurrentNodeIdentity()
	out := health_models.LivenessOutput{
		Status: "alive",
	}
	if id.Role != "" {
		out.Role = id.Role
		out.Datacenter = id.Datacenter
		out.NodeID = id.NodeID
	}
	return out
}
