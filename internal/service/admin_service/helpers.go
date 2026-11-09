package admin_service

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// PublishTrustScoreEvent publie un événement de réputation dans la file Redis du Worker sans bloquer la requête HTTP.
func PublishTrustScoreEvent(ctx context.Context, userID int64, eventType string, targetID int64, reason string) error {
	if userID <= 0 {
		return nil
	}
	evt := moderation_models.TrustScoreEventPayload{
		EventID:      pkg.GenerateID(),
		UserID:       userID,
		EventType:    eventType,
		TargetID:     targetID,
		Reason:       reason,
		OccurredAtMs: domain.NowMillis(),
	}
	payloadBytes, err := json.Marshal(evt)
	if err != nil {
		return numan_error.NewInternal(err)
	}
	if errQueue := redis.WorkerQueue.LPush(ctx, variables.WorkerQueueTrustScore, payloadBytes); errQueue != nil {
		return numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de publier l'événement de réputation dans la file d'attente.",
			errQueue,
		)
	}
	return nil
}

// clampTrustUnit borne strictement le score de confiance entre le minimum et le maximum absolu du système.
func clampTrustUnit(v float64) float64 {
	if v < variables.TrustScoreMin {
		return variables.TrustScoreMin
	}
	if v > variables.TrustScoreMax {
		return variables.TrustScoreMax
	}
	return v
}

// ToPublicTicketView convertit un enregistrement interne de ticket de modération en DTO sécurisé.
// Puisque ce helper est dans admin_service, on part du principe que l'Admin a souvent accès
// à l'historique d'audit (includeAdminAuditDetails = true).
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

	// Masquage de l'identité des modérateurs pour l'anonymat lors des revues croisées.
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
