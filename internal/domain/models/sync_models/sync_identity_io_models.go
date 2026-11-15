package sync_models

import (
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type SyncIdentityInput struct {
	ProfileUpdatedAt  int64 `json:"profile_updated_at"`
	SettingsUpdatedAt int64 `json:"settings_updated_at"`
}

// Validate vérifie les horodatages client pour la synchronisation d'identité.
func (p *SyncIdentityInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de synchronisation d'identité est nul.", nil)
	}
	if p.ProfileUpdatedAt < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'horodatage de profil ne peut pas être négatif.", nil)
	}
	if p.SettingsUpdatedAt < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'horodatage des paramètres ne peut pas être négatif.", nil)
	}
	return nil
}

type SyncIdentityOutput struct {
	ProfileUpdated  bool                                     `json:"profile_updated"`
	Profile         auth_models.UserProfileView              `json:"profile"` // ✅ Remplacé par le DTO sécurisé
	Avatar          media_models.MediaView                   `json:"avatar"`
	SettingsUpdated bool                                     `json:"settings_updated"`
	Settings        user_settings_models.UserSettingsPayload `json:"settings"`
}
