package moderation_models

type ResolveAdminAuditInput struct {
	ReportID              int64   `json:"report_id" binding:"required,gt=0"`
	Decision              string  `json:"decision" binding:"required,oneof=validate revoke"`
	Rationale             string  `json:"rationale" binding:"required,min=5,max=1000"`
	RevokedBlameReportIDs []int64 `json:"revoked_blame_report_ids,omitempty"`
}

type ResolveAdminAuditOutput struct {
	Ticket          ModerationTicketPublicView `json:"ticket"`
	TargetBanned    bool                       `json:"target_banned"`
	PenalizedModIDs []int64                    `json:"penalized_mod_ids,omitempty"`
	DemotedModIDs   []int64                    `json:"demoted_mod_ids,omitempty"`
	RewardedModIDs  []int64                    `json:"rewarded_mod_ids,omitempty"`
}
