package user_recommendation_models

import (
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # MODÈLES DE DOMAINE : RECOMMANDATION D'UTILISATEURS
// ############################################################################

// GetUserRecommendationsInput représente le corps JSON de POST /users/recommendations (et POST /users/recommendations/force).
type GetUserRecommendationsInput struct {
	Sort  string `json:"sort" binding:"omitempty,oneof=global geo aff taste" example:"global"`
	Limit int64  `json:"limit" binding:"omitempty,min=1,max=50" example:"20"`
	Force bool   `json:"-" swaggerignore:"true"` // Défini explicitement par la variante de route (/users/recommendations vs /users/recommendations/force)
}

// Validate vérifie les invariants métier des paramètres de récupération de recommandations.
func (p *GetUserRecommendationsInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres de recommandation sont nuls.", nil)
	}
	if p.Sort == "" {
		p.Sort = variables.UserRecoSortGlobal
	}
	switch p.Sort {
	case variables.UserRecoSortGlobal, variables.UserRecoSortGeo, variables.UserRecoSortAff, variables.UserRecoSortTaste:
	default:
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le mode de tri demandé est invalide (valeurs autorisées : global, geo, aff, taste).", nil)
	}

	if p.Limit == 0 {
		p.Limit = variables.UserRecoDefaultLimit
	}
	if p.Limit < variables.UserRecoMinLimit || p.Limit > variables.UserRecoMaxLimit {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite demandée doit être comprise entre 1 et 50.", nil)
	}
	return nil
}

// GetUserRecommendationsOutput représente la réponse de POST /users/recommendations (et POST /users/recommendations/force).
type GetUserRecommendationsOutput struct {
	Users []auth_models.UserLiteView `json:"users"`
}

// SkipUserRecommendationInput représente le corps JSON de POST /users/recommendations/skips.
type SkipUserRecommendationInput struct {
	SkippedUserID int64 `json:"skipped_user_id" binding:"required,gt=0" example:"12345"`
}

// Validate vérifie la validité de l'identifiant utilisateur ignoré.
func (p *SkipUserRecommendationInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload d'exclusion est nul.", nil)
	}
	if p.SkippedUserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de l'utilisateur à ignorer doit être strictement positif.", nil)
	}
	return nil
}

// SkipUserRecommendationOutput représente la réponse de POST /users/recommendations/skips.
type SkipUserRecommendationOutput struct {
	Status        string `json:"status" example:"ok"`
	SkippedUserID int64  `json:"skipped_user_id" example:"12345"`
}

// UserRecoSkipPayload représente un événement de skip persisté dans MongoDB (collection user_reco_skips).
type UserRecoSkipPayload struct {
	UserID        int64     `json:"user_id" bson:"user_id" msgpack:"user_id"`
	SkippedUserID int64     `json:"skipped_user_id" bson:"skipped_user_id" msgpack:"skipped_user_id"`
	CreatedAt     time.Time `json:"created_at" bson:"created_at" msgpack:"created_at"`
}

// Validate vérifie l'intégrité du document UserRecoSkipPayload avant persistance.
func (p *UserRecoSkipPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de skip est nul.", nil)
	}
	if p.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur source est invalide.", nil)
	}
	if p.SkippedUserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur ignoré est invalide.", nil)
	}
	if p.UserID == p.SkippedUserID {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Un utilisateur ne peut pas s'ignorer lui-même.", nil)
	}
	if p.CreatedAt.IsZero() {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La date de création du skip est obligatoire.", nil)
	}
	return nil
}

// GenerateUserRecommendationsJobPayload représente la charge utile du job asynchrone ActionGenerateUserRecommendations.
type GenerateUserRecommendationsJobPayload struct {
	UserID      int64  `json:"user_id"`
	Force       bool   `json:"force"`
	TriggeredBy string `json:"triggered_by"`
	RequestedAt int64  `json:"requested_at"`
}

// Validate vérifie que le job asynchrone de génération de recommandations cible un utilisateur valide.
func (p *GenerateUserRecommendationsJobPayload) Validate() error {
	if p == nil || p.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur cible du calcul de recommandation est invalide.", nil)
	}
	return nil
}
