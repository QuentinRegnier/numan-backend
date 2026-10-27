package moderation_models

type RequestTicketInfoInput struct {
	ReportID int64  `json:"report_id" binding:"required,gt=0"`
	Question string `json:"question" binding:"required,min=3,max=1000"`
}
