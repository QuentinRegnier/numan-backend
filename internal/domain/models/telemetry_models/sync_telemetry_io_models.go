package telemetry_models

type SyncTelemetryInput struct {
	Payload SyncTelemetryPayload `json:"payload"`
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
