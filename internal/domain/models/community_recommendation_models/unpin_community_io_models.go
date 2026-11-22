package community_recommendation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// UnpinCommunityInput représente le corps JSON de POST /admin/communities/unpin.
type UnpinCommunityInput struct {
	ConversationID int64 `json:"conversation_id" binding:"required"`
}

// Validate vérifie l'identifiant de conversation.
func (in *UnpinCommunityInput) Validate() error {
	if in == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de désépinglage est nul.", nil)
	}
	if in.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	return nil
}

// UnpinCommunityOutput représente la réponse JSON de POST /admin/communities/unpin.
type UnpinCommunityOutput struct {
	ConversationID int64 `json:"conversation_id"`
	Unpinned       bool  `json:"unpinned"`
}
