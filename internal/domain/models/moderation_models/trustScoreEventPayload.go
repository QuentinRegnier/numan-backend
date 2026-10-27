package moderation_models

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
