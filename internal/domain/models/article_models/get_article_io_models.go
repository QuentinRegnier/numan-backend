package article_models

import (
	"fmt"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// GetArticleInput représente la requête de lecture des métadonnées d'un article unique par son identifiant.
type GetArticleInput struct {
	ArticleID int64 `json:"article_id"`
	ID        int64 `json:"id,omitempty"`
}

// ResolvedID retourne l'identifiant de l'article (`article_id` ou alias `id`).
func (in GetArticleInput) ResolvedID() int64 {
	if in.ArticleID > 0 {
		return in.ArticleID
	}
	return in.ID
}

// Validate vérifie que l'identifiant d'article fourni est strictement positif.
func (in GetArticleInput) Validate() error {
	if in.ResolvedID() <= 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'identifiant de l'article (article_id) est requis et doit être strictement positif.",
			fmt.Errorf("invalid article_id: %d", in.ResolvedID()),
		)
	}
	return nil
}

// GetArticleOutput encapsule la vue détaillée d'un article.
type GetArticleOutput struct {
	Article ArticleDetailView `json:"article"`
}
