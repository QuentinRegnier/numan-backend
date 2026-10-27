package moderation_models

type ReplyTicketInfoInput struct {
	ReportID int64  `json:"report_id" binding:"required,gt=0"`
	Reply    string `json:"reply" binding:"required,min=1,max=2000"`
}
