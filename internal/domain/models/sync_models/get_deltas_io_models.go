package sync_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// GetDeltasInput valide la requête POST entrante.
// Zéro paramètre d'URL : le timestamp est dans le JSON.
type GetDeltasInput struct {
	SinceMs int64 `json:"since_ms" binding:"min=0"`
}

// Validate vérifie le timestamp de départ pour la récupération des deltas.
func (p *GetDeltasInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de récupération des deltas est nul.", nil)
	}
	if p.SinceMs < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le timestamp since_ms ne peut pas être négatif.", nil)
	}
	return nil
}

// GetDeltasOutput structure la réponse.
type GetDeltasOutput struct {
	ModifiedConversationIDs []int64 `json:"modified_conversation_ids"`
}
