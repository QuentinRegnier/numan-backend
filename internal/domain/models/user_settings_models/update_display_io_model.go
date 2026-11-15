package user_settings_models

// UpdateDisplayInput valide les modifications d'affichage et de contenu
type UpdateDisplayInput struct {
	Theme          int  `json:"theme" binding:"omitempty,oneof=0 1 2"`
	Language       int  `json:"language" binding:"omitempty,min=0"`
	SafeForCommute bool `json:"safe_for_commute" binding:"omitempty"` // NOUVEAU
}

// Validate vérifie la validité des paramètres d'affichage et de contenu.
func (p *UpdateDisplayInput) Validate() error {
	if p == nil {
		var nilSettings *DisplayAndContentSettings
		return nilSettings.Validate()
	}
	settings := DisplayAndContentSettings{
		Theme:          p.Theme,
		Language:       p.Language,
		SafeForCommute: p.SafeForCommute,
	}
	return settings.Validate()
}

type UpdateDisplayOutput struct {
	UserSettingsUpdateAt int64 `json:"user_settings_update_at"`
}
