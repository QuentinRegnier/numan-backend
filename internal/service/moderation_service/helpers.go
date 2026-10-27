package moderation_service

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
)

// ToPublicTicketView convertit un enregistrement interne de ticket de modération en DTO sécurisé selon le grade de l'appelant.
// Seul un Administrateur (includeAdminAuditDetails == true) a accès à l'historique complet d'audit des blâmes.
// Masque strictement ClaimedByModID et ProposerPseudonym pour les pairs modérateurs lors des étapes anonymisées (§C.10.2 & §B.1.5).
func ToPublicTicketView(r *moderation_models.ModerationTicketRecord, includeAdminAuditDetails bool) moderation_models.ModerationTicketPublicView {
	if r == nil {
		return moderation_models.ModerationTicketPublicView{}
	}
	view := moderation_models.ModerationTicketPublicView{
		ReportID:           r.ReportID,
		TargetType:         r.TargetType,
		TargetID:           r.TargetID,
		TargetOwnerID:      r.TargetOwnerID,
		Category:           r.Category,
		ReportOrigin:       r.ReportOrigin,
		State:              r.State,
		Reason:             r.Reason,
		PriorityScore:      r.PriorityScore,
		IsInfiniteUrgent:   r.IsInfiniteUrgent,
		ClaimedByModID:     r.ClaimedByModID,
		LockExpiresAtMs:    r.LockExpiresAtMs,
		InfoRequestMessage: r.InfoRequestMessage,
		InfoReplyMessage:   r.InfoReplyMessage,
		WaitingUntilMs:     r.WaitingUntilMs,
		ProposerPseudonym:  r.ProposerPseudonym,
		BlameMotivation:    r.BlameMotivation,
		ReviewerRationale:  r.ReviewerRationale,
		EscalationType:     r.EscalationType,
		EscalationReason:   r.EscalationReason,
		CGUFeedbackNote:    r.CGUFeedbackNote,
		ResolutionOutcome:  r.ResolutionOutcome,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
	// SÉCURITÉ ANONYMAT (§C.10.2 & §B.1.5) : En revue par les pairs (state 3) ou vote final (state 5), masquer ClaimedByModID et ProposerPseudonym pour tout non-admin
	if (!includeAdminAuditDetails && (r.State == 3 || r.State == 5)) || r.State == 3 {
		view.ClaimedByModID = 0
	}
	if !includeAdminAuditDetails {
		view.ProposerPseudonym = ""
	}
	if includeAdminAuditDetails {
		view.AuditBlameRecords = r.AuditBlameRecords
	}
	return view
}
