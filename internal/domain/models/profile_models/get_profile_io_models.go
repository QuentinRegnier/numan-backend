package profile_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// GetProfileInput valide la demande du client.
// L'utilisation d'un JSON évite les paramètres d'URL dégueulasses.
type GetProfileInput struct {
	TargetID int64 `json:"target_id" binding:"omitempty"` // Si vide ou 0, on charge le profil de l'appelant
	Limit    int64 `json:"limit" binding:"omitempty,min=1,max=50"`
	Offset   int64 `json:"offset" binding:"omitempty,min=0"`
}

// Validate vérifie les invariants métier de GetProfileInput.
func (p *GetProfileInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	if p.TargetID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant cible ne peut pas être négatif.", nil)
	}
	if p.Limit < 0 || p.Limit > 50 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite doit être comprise entre 0 et 50.", nil)
	}
	if p.Offset < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'offset ne peut pas être négatif.", nil)
	}
	return nil
}

// GetProfileOutput est la super-structure agglomérant tous les domaines.
type GetProfileOutput struct {
	User                   auth_models.UserProfileView `json:"user"`
	Avatar                 media_models.MediaView      `json:"avatar"`
	RelationViewerToTarget int                         `json:"relation_viewer_to_target"`        // Ce que l'appelant pense de la cible (0, 1, 2, -1)
	RelationTargetToViewer int                         `json:"relation_target_to_viewer"`        // Ce que la cible pense de l'appelant (0, 1, 2, -1)
	DirectConversationID   int64                       `json:"direct_conversation_id,omitempty"` // 0 si aucune conversation existante
	Posts                  []post_models.GetPostOutput `json:"posts"`
	LikedPostIDs           []int64                     `json:"liked_post_ids"`
	SavedPostIDs           []int64                     `json:"saved_post_ids"`
}
