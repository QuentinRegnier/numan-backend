package search_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// UserSearchInput valide les paramètres de la requête GET.
// On utilise 'form' pour binder les query parameters (?q=...&limit=...).
type UserSearchInput struct {
	Prefix string `form:"q" json:"q" binding:"required"`
	Offset int64  `form:"offset" json:"offset" binding:"gte=0"`
	Limit  int64  `form:"limit,default=20" json:"limit" binding:"omitempty,min=1,max=50"`
}

// Validate vérifie les invariants métier d'une recherche d'utilisateurs.
func (p *UserSearchInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de recherche d'utilisateurs est nulle.", nil)
	}
	trimmed := strings.TrimSpace(p.Prefix)
	if trimmed == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le préfixe de recherche d'utilisateur est obligatoire.", nil)
	}
	if utf8.RuneCountInString(trimmed) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le préfixe de recherche ne peut pas dépasser 100 caractères.", nil)
	}
	if p.Offset < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'offset de pagination ne peut pas être négatif.", nil)
	}
	if p.Limit < 0 || p.Limit > 50 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite de pagination doit être comprise entre 1 et 50.", nil)
	}
	return nil
}

// UserSearchOutput est la réponse riche renvoyée au client mobile.
// On réutilise la structure UserLiteView existante.
type UserSearchOutput struct {
	Users []auth_models.UserLiteView `json:"users"`
}
