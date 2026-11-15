package comment_models

import (
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// Structure interne correspondant exactement au schéma Postgres content.comments
type CommentPayload struct {
	ID         int64  `json:"id"`
	PostID     int64  `json:"post_id"`
	UserID     int64  `json:"user_id"`
	Content    string `json:"content"`
	Visibility int    `json:"visibility"`
	LikeCount  int    `json:"like_count" bson:"like_count"` // <-- Ligne à ajouter
	Score      int    `json:"score" bson:"score"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// Validate vérifie les invariants métier d'un CommentPayload avant mise en cache ou persistance.
func (p *CommentPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload du commentaire est nul.", nil)
	}
	if p.ID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du commentaire doit être strictement positif.", nil)
	}
	if p.PostID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la publication doit être strictement positif.", nil)
	}
	if p.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de l'auteur doit être strictement positif.", nil)
	}
	if strings.TrimSpace(p.Content) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le contenu du commentaire ne peut pas être vide.", nil)
	}
	if p.Visibility != 0 && p.Visibility != variables.CommentVisibilitySoftDelete {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le statut de visibilité du commentaire est invalide.", nil)
	}
	if p.LikeCount < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nombre de likes d'un commentaire ne peut pas être négatif.", nil)
	}
	return nil
}
