package moderation_models

type AdminDelegationSwitchInput struct {
	Enabled bool `json:"enabled"`
}

type AdminDelegationSwitchOutput struct {
	Enabled   bool  `json:"enabled"`
	UpdatedAt int64 `json:"updated_at"`
}
