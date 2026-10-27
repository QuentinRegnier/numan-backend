package moderation_models

type ReviewBlameInput struct {
	ReportID  int64  `json:"report_id" binding:"required,gt=0"`
	Decision  string `json:"decision" binding:"required,oneof=validate oppose"`
	Rationale string `json:"rationale" binding:"required,min=3,max=1000"`
}
