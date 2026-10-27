package moderation_models

type ProposeBlameInput struct {
	ReportID   int64  `json:"report_id" binding:"required,gt=0"`
	Motivation string `json:"motivation" binding:"required,min=5,max=1000"`
}
