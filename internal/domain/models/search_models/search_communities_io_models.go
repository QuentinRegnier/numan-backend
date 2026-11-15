package search_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// CommunitySearchInput valide les paramètres de la requête GET.
// On utilise 'form' pour binder les query parameters (?q=...&limit=...).
type CommunitySearchInput struct {
	Prefix string `form:"q" json:"q" binding:"required"`
	Offset int64  `form:"offset" json:"offset" binding:"gte=0"`
	Limit  int64  `form:"limit,default=20" json:"limit" binding:"omitempty,min=1,max=50"`
}

// Validate vérifie les invariants métier d'une recherche de communautés.
func (p *CommunitySearchInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de recherche de communautés est nulle.", nil)
	}
	trimmed := strings.TrimSpace(p.Prefix)
	if trimmed == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le préfixe de recherche de communauté est obligatoire.", nil)
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

// CommunitySearchOutput est la réponse riche renvoyée au client mobile.
// On réutilise la structure CommunityLiteView existante.
type CommunitySearchOutput struct {
	Communities []conversation_models.CommunityLiteView `json:"communities"`
}
