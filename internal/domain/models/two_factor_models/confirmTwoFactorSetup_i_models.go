package two_factor_models

type ConfirmTwoFactorSetupInput struct {
	SetupToken          string                       `json:"setup_token" binding:"required"`
	Code                string                       `json:"code" binding:"omitempty,len=6,numeric"`
	PasskeyRegistration *PasskeyRegistrationResponse `json:"passkey_registration" binding:"omitempty"`
}
