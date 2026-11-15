package post_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/comment_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type GetPostInput struct {
	PostIDs []int64 `json:"post_ids" binding:"required,min=1,max=50"`
}

// Validate vérifie la validité de la liste d'identifiants de publications demandées.
func (p *GetPostInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de récupération des publications est nulle.", nil)
	}
	if len(p.PostIDs) == 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La liste des publications demandées est vide.", nil)
	}
	if len(p.PostIDs) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La liste des publications demandées dépasse la limite autorisée (100).", nil)
	}
	for _, id := range p.PostIDs {
		if id <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Tous les identifiants de publications doivent être strictement positifs.", nil)
		}
	}
	return nil
}

type GetPostOutput struct {
	PostID         int64                             `json:"post_id"`
	Data           PostPayload                       `json:"data,omitempty"`
	AuthorUsername string                            `json:"author_username,omitempty"` // ✅ NOUVEAU
	AuthorAvatar   media_models.MediaView            `json:"author_avatar,omitempty"`   // ✅ NOUVEAU
	Media          []media_models.MediaView          `json:"media,omitempty"`
	Comments       []comment_models.GetCommentOutput `json:"comments,omitempty"`
	Error          string                            `json:"numan_error,omitempty"`
}
