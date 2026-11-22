package community_recommendation_models

import (
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// GetCommunityRecommendationsInput représente le corps JSON de POST /communities/recommendations.
// Limit == 0 applique la valeur par défaut (20) ; Offset == 0 commence au début du classement.
type GetCommunityRecommendationsInput struct {
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
	Lang   string `json:"lang"`
}

// Validate vérifie les bornes de pagination et la validité éventuelle de la langue explicite.
func (in *GetCommunityRecommendationsInput) Validate() error {
	if in == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de recommandation de communautés est nul.", nil)
	}
	if in.Limit < 0 || in.Limit > variables.CommunityRecoMaxLimit {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le paramètre limit doit être compris entre 1 et 50.", nil)
	}
	if in.Offset < variables.CommunityRecoMinOffset || in.Offset > variables.CommunityRecoMaxOffset {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le paramètre offset doit être compris entre 0 et 500.", nil)
	}
	if strings.TrimSpace(in.Lang) != "" {
		if _, ok := variables.NormalizeCommunityLang(in.Lang); !ok {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La langue demandée n'est pas supportée.", nil)
		}
	}
	return nil
}

// EffectiveLimit retourne la limite effective (par défaut 20 si 0).
func (in GetCommunityRecommendationsInput) EffectiveLimit() int {
	if in.Limit == 0 {
		return variables.CommunityRecoDefaultLimit
	}
	return in.Limit
}

// EffectiveOffset retourne le décalage effectif (par défaut 0).
func (in GetCommunityRecommendationsInput) EffectiveOffset() int {
	return in.Offset
}

// GetCommunityRecommendationsOutput représente la réponse JSON de POST /communities/recommendations.
type GetCommunityRecommendationsOutput struct {
	Lang        string                        `json:"lang"`
	Total       int                           `json:"total"`
	HasMore     bool                          `json:"has_more"`
	Communities []CommunityRecommendationItem `json:"communities"`
}
