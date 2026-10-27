package moderation_models

type QuarantinedMediaItem struct {
	MediaID       int64   `json:"media_id"`
	OwnerID       int64   `json:"owner_id"`
	OwnerScore    float64 `json:"owner_trust_score"`
	IAScore       float64 `json:"ia_score"`
	PriorityScore float64 `json:"priority_score"`
	SignedURL     string  `json:"signed_url"`
	CreatedAt     int64   `json:"created_at"`
}
