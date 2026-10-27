package moderation_models

// ModerationTicketPublicView est la vue assainie exposée par l'API.
// L'identité réelle du Modérateur A (ProposerModID) n'y figure jamais pour les pairs modérateurs.
type ModerationTicketPublicView struct {
	ReportID           int64         `json:"report_id"`
	TargetType         int           `json:"target_type"`
	TargetID           int64         `json:"target_id"`
	TargetOwnerID      int64         `json:"target_owner_id"`
	Category           int           `json:"category"`
	ReportOrigin       int           `json:"report_origin"`
	State              int           `json:"state"`
	Reason             string        `json:"reason"`
	PriorityScore      float64       `json:"priority_score"`
	IsInfiniteUrgent   bool          `json:"is_infinite_urgent"`
	ClaimedByModID     int64         `json:"claimed_by_mod_id,omitempty"`
	LockExpiresAtMs    int64         `json:"lock_expires_at_ms,omitempty"`
	InfoRequestMessage string        `json:"info_request_message,omitempty"`
	InfoReplyMessage   string        `json:"info_reply_message,omitempty"`
	WaitingUntilMs     int64         `json:"waiting_until_ms,omitempty"`
	ProposerPseudonym  string        `json:"proposer_pseudonym,omitempty"`
	BlameMotivation    string        `json:"blame_motivation,omitempty"`
	ReviewerRationale  string        `json:"reviewer_rationale,omitempty"`
	EscalationType     string        `json:"escalation_type,omitempty"`
	EscalationReason   string        `json:"escalation_reason,omitempty"`
	CGUFeedbackNote    string        `json:"cgu_feedback_note,omitempty"`
	AuditBlameRecords  []BlameRecord `json:"audit_blame_records,omitempty"`
	ResolutionOutcome  string        `json:"resolution_outcome,omitempty"`
	CreatedAt          int64         `json:"created_at"`
	UpdatedAt          int64         `json:"updated_at"`
}
