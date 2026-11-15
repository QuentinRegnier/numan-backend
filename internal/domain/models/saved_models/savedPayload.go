package saved_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// SavedPayload représente l'association entre un utilisateur et un post sauvegardé.
type SavedPayload struct {
	ID        int64 `json:"id" bson:"id"`
	UserID    int64 `json:"user_id" bson:"user_id"`
	PostID    int64 `json:"post_id" bson:"post_id"`
	CreatedAt int64 `json:"created_at" bson:"created_at"`
}

// Validate vérifie les invariants d'une sauvegarde de publication.
func (p *SavedPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de sauvegarde est nul.", nil)
	}
	if p.ID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la sauvegarde doit être strictement positif.", nil)
	}
	if p.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de l'utilisateur doit être strictement positif.", nil)
	}
	if p.PostID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la publication doit être strictement positif.", nil)
	}
	if p.CreatedAt < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La date de création de la sauvegarde est invalide.", nil)
	}
	return nil
}
