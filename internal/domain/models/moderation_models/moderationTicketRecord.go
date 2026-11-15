package moderation_models

import (
	"math"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/report_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

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

// Validate vérifie les invariants d'un ticket interne de modération avant persistance en cache L1.
func (p *ModerationTicketRecord) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le ticket de modération est nul.", nil)
	}
	if p.ReportID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du ticket doit être strictement positif.", nil)
	}
	if !report_models.IsValidReportTargetType(p.TargetType) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Type de cible du ticket invalide.", nil)
	}
	if p.TargetID < 0 || p.TargetOwnerID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les identifiants cibles du ticket ne peuvent pas être négatifs.", nil)
	}
	if p.Category != 0 && !report_models.IsValidReportCategory(p.Category) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Catégorie du ticket invalide.", nil)
	}
	if !report_models.IsValidReportOrigin(p.ReportOrigin) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Origine du ticket invalide.", nil)
	}
	if !report_models.IsValidReportState(p.State) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "État du ticket invalide.", nil)
	}
	if math.IsNaN(p.StaticBaseScore) || math.IsInf(p.StaticBaseScore, -1) || p.StaticBaseScore < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le score statique de base du ticket est invalide.", nil)
	}
	if math.IsNaN(p.PriorityScore) || math.IsInf(p.PriorityScore, -1) || p.PriorityScore < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le score de priorité du ticket est invalide.", nil)
	}
	for _, repID := range p.ReporterIDs {
		if repID <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Chaque identifiant de signaleur doit être strictement positif.", nil)
		}
	}
	if p.ClaimedByModID < 0 || p.ProposerModID < 0 || p.ReviewerModID < 0 || p.FinalVoterModID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les identifiants modérateurs du ticket ne peuvent pas être négatifs.", nil)
	}
	if p.LockExpiresAtMs < 0 || p.WaitingUntilMs < 0 || p.CreatedAt < 0 || p.UpdatedAt < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les horodatages du ticket ne peuvent pas être négatifs.", nil)
	}
	if utf8.RuneCountInString(p.Reason) > 1000 ||
		utf8.RuneCountInString(p.BlameMotivation) > 1000 ||
		utf8.RuneCountInString(p.ReviewerRationale) > 1000 ||
		utf8.RuneCountInString(p.EscalationReason) > 1000 ||
		utf8.RuneCountInString(p.CGUFeedbackNote) > 2000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Un champ textuel du ticket dépasse la longueur maximale autorisée.", nil)
	}
	for i := range p.AuditBlameRecords {
		if err := p.AuditBlameRecords[i].Validate(); err != nil {
			return err
		}
	}
	return nil
}
