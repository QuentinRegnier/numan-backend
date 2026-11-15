package search_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// SearchPostInput valide les critères de recherche de publications.
type SearchPostInput struct {
	Query  string `json:"query" binding:"omitempty"`
	Filter string `json:"filter" binding:"omitempty,oneof=views likes comments recent oldest trend"`
	Offset int64  `form:"offset" json:"offset" binding:"gte=0"`
	Limit  int64  `json:"limit" binding:"omitempty,min=1,max=50"`
}

// Validate vérifie qu'au moins une requête ou un filtre valide est fourni ainsi que les bornes de pagination.
func (p *SearchPostInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de recherche de publications est nulle.", nil)
	}
	trimmedQuery := strings.TrimSpace(p.Query)
	if trimmedQuery == "" && p.Filter == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Une requête de recherche ou un filtre est obligatoire.", nil)
	}
	if utf8.RuneCountInString(trimmedQuery) > 200 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de recherche ne peut pas dépasser 200 caractères.", nil)
	}
	if p.Filter != "" {
		switch p.Filter {
		case variables.SearchFilterViews,
			variables.SearchFilterLikes,
			variables.SearchFilterComments,
			variables.SearchFilterRecent,
			variables.SearchFilterOldest,
			variables.SearchFilterTrend:
		default:
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le filtre de recherche est invalide.", nil)
		}
	}
	if p.Offset < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'offset de pagination ne peut pas être négatif.", nil)
	}
	if p.Limit < 0 || p.Limit > 50 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite de pagination doit être comprise entre 1 et 50.", nil)
	}
	return nil
}

// SearchPostOutput structure la réponse de l'API.
type SearchPostOutput struct {
	Posts                   []post_models.GetPostOutput `json:"posts"`
	HasTrendFilterAvailable bool                        `json:"has_trend_filter_available"`
}
