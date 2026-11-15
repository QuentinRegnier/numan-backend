package telemetry_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// TelemetryEvent représente un log d'interaction (aligné sur le TDD § 2.3).
type TelemetryEvent struct {
	PostID       int64 `json:"post_id"`
	DwellTimeMs  int   `json:"dwell_time_ms"`
	IsClicked    bool  `json:"is_clicked"`
	DeepScroll   bool  `json:"deep_scroll"`   // NOUVEAU: Scroll > 80% du post
	ProfileVisit bool  `json:"profile_visit"` // NOUVEAU: Clic sur l'avatar de l'auteur
}

// Validate vérifie les invariants métier d'un événement de télémétrie.
func (p *TelemetryEvent) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'événement de télémétrie est nul.", nil)
	}
	if p.PostID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de publication de l'événement doit être strictement positif.", nil)
	}
	if p.DwellTimeMs < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le temps de lecture (dwell_time_ms) ne peut pas être négatif.", nil)
	}
	return nil
}
