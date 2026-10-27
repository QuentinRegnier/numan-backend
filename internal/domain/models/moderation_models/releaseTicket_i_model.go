package moderation_models

type ReleaseTicketInput struct {
	ReportID int64 `json:"report_id" binding:"required,gt=0"`
}
