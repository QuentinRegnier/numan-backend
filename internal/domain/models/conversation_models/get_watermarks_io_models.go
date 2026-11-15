package conversation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// GetWatermarksInput valide la requête POST entrante.
// Zéro paramètre d'URL : l'ID de la conversation est dans le JSON.
type GetWatermarksInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
}

// Validate vérifie l'identifiant de conversation pour les curseurs de lecture.
func (p *GetWatermarksInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload des curseurs de lecture est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	return nil
}

// GetWatermarksOutput structure la réponse.
// On utilise une map avec des clés string car le standard JSON exige des clés sous forme de chaîne,
// même si ce sont des identifiants (user_id).
type GetWatermarksOutput struct {
	Watermarks map[string]int64 `json:"watermarks"`
}
