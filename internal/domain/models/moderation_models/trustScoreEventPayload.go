package moderation_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// TrustScoreEventPayload est l'événement poussé dans la file Redis du Worker de Réputation.
// Supporte à la fois un événement unitaire (UserID) et un événement de masse (UserIDs pour mod_batch_like).
type TrustScoreEventPayload struct {
	EventID      int64   `json:"event_id"`
	UserID       int64   `json:"user_id,omitempty"`
	UserIDs      []int64 `json:"user_ids,omitempty"`
	EventType    string  `json:"event_type"`
	TargetID     int64   `json:"target_id,omitempty"`
	Reason       string  `json:"reason,omitempty"`
	OccurredAtMs int64   `json:"occurred_at_ms"`
}

// IsValidTrustScoreEventType vérifie si eventType correspond à un déclencheur de réputation reconnu.
func IsValidTrustScoreEventType(eventType string) bool {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "quarantine_approved",
		variables.TrustTriggerVerifiedIdentity,
		variables.TrustTriggerSuccessfulAppeal,
		variables.TrustTriggerMonthlyClean,
		variables.TrustTriggerLikedByAdmin,
		variables.TrustTriggerJustifiedReport,
		variables.TrustTriggerModBatchLike,
		variables.TrustTriggerSpamDetectedAuto,
		variables.TrustTriggerMutedInPublicComm,
		variables.TrustTriggerAbusiveReport,
		variables.TrustTriggerMinorInfraction,
		variables.TrustTriggerMajorInfraction:
		return true
	default:
		return false
	}
}

// Validate vérifie l'intégrité d'un événement de réputation avant publication dans la file Redis.
func (p *TrustScoreEventPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'événement de réputation est nul.", nil)
	}
	if p.EventID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de l'événement de réputation doit être strictement positif.", nil)
	}
	if !IsValidTrustScoreEventType(p.EventType) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Type d'événement de réputation inconnu.", nil)
	}
	if p.UserID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur ne peut pas être négatif.", nil)
	}
	if len(p.UserIDs) > 500 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La liste d'utilisateurs ne peut pas dépasser 500 entrées.", nil)
	}
	for _, id := range p.UserIDs {
		if id <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Chaque identifiant utilisateur de l'événement doit être strictement positif.", nil)
		}
	}
	if p.UserID == 0 && len(p.UserIDs) == 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Au moins un utilisateur cible est requis pour l'événement de réputation.", nil)
	}
	if p.TargetID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant cible ne peut pas être négatif.", nil)
	}
	if utf8.RuneCountInString(p.Reason) > 1000 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le motif de l'événement de réputation ne peut pas dépasser 1000 caractères.", nil)
	}
	if p.OccurredAtMs < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'horodatage de l'événement ne peut pas être négatif.", nil)
	}
	return nil
}
