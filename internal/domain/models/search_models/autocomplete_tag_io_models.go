package search_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// AutocompleteTagInput valide le JSON entrant pour la recherche de tags.
type AutocompleteTagInput struct {
	Query  string `json:"query" binding:"required,min=1"`
	Offset int64  `form:"offset" json:"offset" binding:"gte=0"`
	Limit  int64  `json:"limit" binding:"omitempty,min=1,max=50"`
}

// Validate vérifie les invariants métier d'une requête d'autocomplétion de tags.
func (p *AutocompleteTagInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête d'autocomplétion des tags est nulle.", nil)
	}
	trimmed := strings.TrimSpace(p.Query)
	if trimmed == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le préfixe de recherche de tag est obligatoire.", nil)
	}
	if utf8.RuneCountInString(trimmed) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le préfixe de recherche de tag ne peut pas dépasser 100 caractères.", nil)
	}
	if p.Offset < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'offset de pagination ne peut pas être négatif.", nil)
	}
	if p.Limit < 0 || p.Limit > 50 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite de pagination doit être comprise entre 1 et 50.", nil)
	}
	return nil
}

// AutocompleteTagOutput unifie les résultats lexicographiques.
type AutocompleteTagOutput struct {
	Tags []string `json:"tags"`
}
