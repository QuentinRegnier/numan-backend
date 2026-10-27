package moderation_models

// ModerationTicketRecord est l'enregistrement serveur interne stocké dans redis.ModerationTickets.
// ATTENTION SÉCURITÉ (§B.1.5 & §C.10.2) : Ce modèle interne contient ProposerModID et ne doit JAMAIS
// être sérialisé directement vers un pair modérateur (Moderator B). Utiliser ToPublicTicketView().
type ModerationTicketRecord struct {
	ReportID           int64         `msgpack:"report_id" json:"report_id"`
	TargetType         int           `msgpack:"target_type" json:"target_type"`
	TargetID           int64         `msgpack:"target_id" json:"target_id"`
	TargetOwnerID      int64         `msgpack:"target_owner_id" json:"target_owner_id"`
	Category           int           `msgpack:"category" json:"category"`
	ReportOrigin       int           `msgpack:"report_origin" json:"report_origin"`
	State              int           `msgpack:"state" json:"state"`
	Reason             string        `msgpack:"reason" json:"reason"`
	StaticBaseScore    float64       `msgpack:"static_base_score" json:"static_base_score"`
	PriorityScore      float64       `msgpack:"priority_score" json:"priority_score"`
	IsInfiniteUrgent   bool          `msgpack:"is_infinite_urgent" json:"is_infinite_urgent"`
	ReporterIDs        []int64       `msgpack:"reporter_ids" json:"reporter_ids"`
	ClaimedByModID     int64         `msgpack:"claimed_by_mod_id" json:"claimed_by_mod_id"`
	LockExpiresAtMs    int64         `msgpack:"lock_expires_at_ms" json:"lock_expires_at_ms"`
	InfoRequestMessage string        `msgpack:"info_request_message" json:"info_request_message"`
	InfoReplyMessage   string        `msgpack:"info_reply_message" json:"info_reply_message"`
	WaitingUntilMs     int64         `msgpack:"waiting_until_ms" json:"waiting_until_ms"`
	ProposerModID      int64         `msgpack:"proposer_mod_id" json:"-"` // Masqué en JSON par sécurité
	ProposerPseudonym  string        `msgpack:"proposer_pseudonym" json:"proposer_pseudonym"`
	BlameMotivation    string        `msgpack:"blame_motivation" json:"blame_motivation"`
	ReviewerModID      int64         `msgpack:"reviewer_mod_id" json:"-"` // Masqué en JSON par sécurité
	ReviewerRationale  string        `msgpack:"reviewer_rationale" json:"reviewer_rationale"`
	FinalVoterModID    int64         `msgpack:"final_voter_mod_id" json:"-"`
	EscalationType     string        `msgpack:"escalation_type" json:"escalation_type"` // "blame_contradiction", "blame_quota_audit", "hard_ban_request", "cgu_gray_area"
	EscalationReason   string        `msgpack:"escalation_reason" json:"escalation_reason"`
	CGUFeedbackNote    string        `msgpack:"cgu_feedback_note" json:"cgu_feedback_note"`
	AuditBlameRecords  []BlameRecord `msgpack:"audit_blame_records" json:"audit_blame_records"`
	ResolutionOutcome  string        `msgpack:"resolution_outcome" json:"resolution_outcome"`
	CreatedAt          int64         `msgpack:"created_at" json:"created_at"`
	UpdatedAt          int64         `msgpack:"updated_at" json:"updated_at"`
}
