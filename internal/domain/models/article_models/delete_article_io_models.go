package article_models

import (
	"fmt"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// DeleteArticleInput représente la requête de suppression logique d'un ou plusieurs articles (Admin).
type DeleteArticleInput struct {
	ArticleID         int64   `json:"article_id,omitempty"`
	ID                int64   `json:"id,omitempty"`
	ArticleIDs        []int64 `json:"article_ids,omitempty"`
	ExpectedUpdatedAt *int64  `json:"expected_updated_at,omitempty"`
}

// ResolvedIDs retourne la liste dédupliquée des identifiants d'articles à supprimer.
func (in DeleteArticleInput) ResolvedIDs() ([]int64, error) {
	if in.ExpectedUpdatedAt != nil && *in.ExpectedUpdatedAt <= 0 {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'horodatage de contrôle de concurrence (expected_updated_at) doit être strictement positif.",
			fmt.Errorf("invalid expected_updated_at: %d", *in.ExpectedUpdatedAt),
		)
	}
	raw := make([]int64, 0, len(in.ArticleIDs)+2)
	if in.ArticleID > 0 {
		raw = append(raw, in.ArticleID)
	} else if in.ArticleID < 0 {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'identifiant de l'article doit être strictement positif.",
			fmt.Errorf("negative article_id: %d", in.ArticleID),
		)
	}
	if in.ID > 0 {
		raw = append(raw, in.ID)
	} else if in.ID < 0 {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"L'identifiant de l'article doit être strictement positif.",
			fmt.Errorf("negative id: %d", in.ID),
		)
	}
	raw = append(raw, in.ArticleIDs...)

	if len(raw) == 0 {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Au moins un identifiant d'article (article_id ou article_ids) est requis.",
			fmt.Errorf("empty article IDs in DeleteArticleInput"),
		)
	}

	seen := make(map[int64]struct{}, len(raw))
	dedup := make([]int64, 0, len(raw))
	for _, id := range raw {
		if id <= 0 {
			return nil, numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				"Tous les identifiants d'articles doivent être strictement positifs.",
				fmt.Errorf("non-positive article ID: %d", id),
			)
		}
		if _, exists := seen[id]; !exists {
			seen[id] = struct{}{}
			dedup = append(dedup, id)
		}
	}

	return dedup, nil
}

// DeleteArticleOutput représente le résultat de la suppression logique d'articles.
type DeleteArticleOutput struct {
	DeletedCount       int     `json:"deleted_count"`
	ArticleIDs         []int64 `json:"article_ids"`
	DeactivatedMediaID []int64 `json:"deactivated_media_ids,omitempty"`
}
