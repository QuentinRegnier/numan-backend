package moderation_models

// ReviewQuarantineInput valide la décision d'un modérateur sur un média/post en quarantaine.
type ReviewQuarantineInput struct {
	MediaID  int64  `json:"media_id" binding:"required"`
	PostID   int64  `json:"post_id" binding:"omitempty"`
	Decision string `json:"decision" binding:"required,oneof=approve reject_minor reject_major"`
	Reason   string `json:"reason" binding:"max=500"`
}

type ReviewQuarantineOutput struct {
	MediaID       int64  `json:"media_id"`
	NewVisibility int    `json:"new_visibility"`
	TrustEvent    string `json:"trust_event"`
	UpdatedAt     int64  `json:"updated_at"`
}
