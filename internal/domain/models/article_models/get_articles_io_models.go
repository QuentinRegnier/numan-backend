package article_models

import (
	"fmt"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// GetArticlesInput représente les paramètres de pagination et de filtrage pour la liste des articles.
type GetArticlesInput struct {
	Limit          int  `json:"limit"`
	Offset         int  `json:"offset"`
	IncludeDrafts  bool `json:"include_drafts,omitempty"`
	IncludeExpired bool `json:"include_expired,omitempty"`
}

// ValidateAndNormalize applique les bornes par défaut et vérifie les paramètres de pagination.
func (in *GetArticlesInput) ValidateAndNormalize() error {
	if in.Offset < 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'offset de pagination ne peut pas être négatif.",
			fmt.Errorf("negative offset: %d", in.Offset),
		)
	}
	if in.Limit < 0 {
		return numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"La limite de pagination ne peut pas être négative.",
			fmt.Errorf("negative limit: %d", in.Limit),
		)
	}
	if in.Limit == 0 {
		in.Limit = variables.DefaultArticleListLimit
	}
	if in.Limit > variables.MaxArticleListLimit {
		in.Limit = variables.MaxArticleListLimit
	}
	return nil
}

// GetArticlesOutput représente la réponse paginée légère de la liste des articles.
type GetArticlesOutput struct {
	Articles []ArticleListItemView `json:"articles"`
	Total    int                   `json:"total"`
	Limit    int                   `json:"limit"`
	Offset   int                   `json:"offset"`
}
