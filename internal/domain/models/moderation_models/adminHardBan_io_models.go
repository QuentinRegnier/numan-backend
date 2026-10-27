package moderation_models

type AdminHardBanInput struct {
	TargetUserID    int64  `json:"target_user_id" binding:"required,gt=0"`
	ReportID        int64  `json:"report_id,omitempty"`
	Reason          string `json:"reason" binding:"required,min=5,max=1000"`
	CGUFeedbackNote string `json:"cgu_feedback_note,omitempty" binding:"omitempty,max=2000"`
}

type AdminHardBanOutput struct {
	TargetUserID    int64   `json:"target_user_id"`
	Banned          bool    `json:"banned"`
	TrustScore      float64 `json:"trust_score"`
	CGUFeedbackNote string  `json:"cgu_feedback_note,omitempty"`
	ExecutedAt      int64   `json:"executed_at"`
}
