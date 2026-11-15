package like_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type GetPostLikesInput struct {
	CallerID int64 `json:"caller_id"` // Injecté par le Handler (Sécurité)
	PostID   int64 `json:"post_id" binding:"required"`
	Limit    int64 `json:"limit" binding:"omitempty,min=1,max=100"`
	Offset   int64 `json:"offset" binding:"min=0"`
}

// Validate vérifie les paramètres de pagination et l'identifiant du post.
func (p *GetPostLikesInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres de récupération des likes sont nuls.", nil)
	}
	if p.CallerID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de l'appelant est invalide.", nil)
	}
	if p.PostID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la publication doit être strictement positif.", nil)
	}
	if p.Offset < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'offset de pagination doit être positif ou nul.", nil)
	}
	if p.Limit < 0 || p.Limit > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite de pagination doit être comprise entre 1 et 100.", nil)
	}
	return nil
}

type GetPostLikesOutput struct {
	PostID int64                      `json:"post_id"`
	Users  []auth_models.UserLiteView `json:"users"` // ✅ Remplacé : on renvoie les données prêtes à afficher
}
