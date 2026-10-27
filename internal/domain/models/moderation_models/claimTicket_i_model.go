package moderation_models

type ClaimTicketInput struct {
	ReportID int64 `json:"report_id" binding:"required,gt=0"`
}
