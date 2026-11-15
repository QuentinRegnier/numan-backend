package telemetry_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type SyncTelemetryInput struct {
	Payload SyncTelemetryPayload `json:"payload"`
}

// Validate vérifie la validité du payload de synchronisation télémétrique.
func (p *SyncTelemetryInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'entrée de synchronisation télémétrique est nulle.", nil)
	}
	return p.Payload.Validate()
}

type SyncTelemetryOutput struct {
	Status           string    `json:"status"`
	NeedUpdate       bool      `json:"need_update"`
	ServerVector     []float32 `json:"server_vector"`
	ServerTags       []string  `json:"server_tags"`
	ServerTrustScore float64   `json:"server_trust_score"`
	TrustTier        int       `json:"trust_tier"` // 0: Banni (0.00), 1: Quarantaine (<0.40), 2: Standard (0.40-0.79), 3: VIP (>=0.80)
	ServerUpdated    int64     `json:"server_updated"`
}
