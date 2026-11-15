package sync_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

type SyncPresenceInput struct {
	UserIDs []int64 `json:"user_ids" binding:"required,max=50"` // Max 50 pour éviter le spam réseau
}

// Validate vérifie que la liste d'utilisateurs contient entre 1 et 50 identifiants positifs.
func (p *SyncPresenceInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de présence est nul.", nil)
	}
	if len(p.UserIDs) == 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La liste des identifiants utilisateurs ne peut pas être vide.", nil)
	}
	if len(p.UserIDs) > 50 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La liste ne peut pas dépasser 50 identifiants utilisateurs.", nil)
	}
	for _, id := range p.UserIDs {
		if id <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Tous les identifiants utilisateurs doivent être positifs.", nil)
		}
	}
	return nil
}
