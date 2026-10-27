package moderation_models

type CloseTicketInput struct {
	ReportID  int64  `json:"report_id" binding:"required,gt=0"`
	IsDismiss bool   `json:"is_dismiss"`
	Rationale string `json:"rationale" binding:"required,min=3,max=1000"`
}

type CloseTicketOutput struct {
	ReportID   int64  `json:"report_id"`
	State      int    `json:"state"`
	Outcome    string `json:"outcome"`
	ArchivedAt int64  `json:"archived_at"`
}
