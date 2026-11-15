package moderation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type ClaimTicketInput struct {
	ReportID int64 `json:"report_id" binding:"required,gt=0"`
}

// Validate vérifie l'identifiant du ticket à réclamer.
func (p *ClaimTicketInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de prise en charge est vide.", nil)
	}
	if p.ReportID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du signalement doit être strictement positif.", nil)
	}
	return nil
}
