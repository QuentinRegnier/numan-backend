package moderation_models

type EscalateTicketInput struct {
	ReportID        int64  `json:"report_id" binding:"required,gt=0"`
	EscalationType  string `json:"escalation_type" binding:"required,oneof=hard_ban_request cgu_gray_area"`
	Reason          string `json:"reason" binding:"required,min=5,max=1000"`
	CGUFeedbackNote string `json:"cgu_feedback_note,omitempty" binding:"omitempty,max=2000"`
}
