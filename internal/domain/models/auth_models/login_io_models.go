package auth_models

type LoginInput struct {
	Email                  string                 `json:"email" binding:"required,email" example:"john@numan.com"`
	PasswordHash           string                 `json:"password_hash" binding:"required" example:"hashed_secret_123"`
	DeviceInfo             map[string]interface{} `json:"device_info" swaggertype:"object"`
	FirebaseInstallationID string                 `json:"firebase_installation_id" binding:"required" example:"eyJhbGciOiJIUzI1Ni..."`
}

type LoginResponse struct {
	RequiresTwoFactor bool   `json:"requires_two_factor"`
	TwoFactorMethod   int    `json:"two_factor_method,omitempty"`
	AvailableMethods  []int  `json:"available_methods,omitempty"` // Liste des méthodes configurées (ex: [1, 2, 3])
	PreAuthToken      string `json:"pre_auth_token,omitempty"`
	PasskeyOptions    any    `json:"passkey_options,omitempty"`
	UserID            int64  `json:"user_id,omitempty" example:"42"`
	MasterToken       string `json:"master_token,omitempty"`
	JWT               string `json:"jwt,omitempty" example:"eyJhbGciOiJIUzI1Ni..."`
	ExpiresAt         int64  `json:"expires_at,omitempty"`
	Message           string `json:"message" example:"Login successful"`
}
