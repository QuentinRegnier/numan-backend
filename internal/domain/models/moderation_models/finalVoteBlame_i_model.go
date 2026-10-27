package moderation_models

type FinalVoteBlameInput struct {
	ReportID  int64  `json:"report_id" binding:"required,gt=0"`
	Decision  string `json:"decision" binding:"required,oneof=confirm_blame reject_blame"`
	Rationale string `json:"rationale" binding:"required,min=3,max=1000"`
}
