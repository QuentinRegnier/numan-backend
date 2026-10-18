package two_factor_models

type PasskeyRegistrationResponse struct {
	CredentialID      string   `json:"credential_id" binding:"required"`
	PublicKeyCOSE     string   `json:"public_key_cose" binding:"required"`
	AuthenticatorData string   `json:"authenticator_data" binding:"required"`
	ClientDataJSON    string   `json:"client_data_json" binding:"required"`
	Transports        []string `json:"transports" binding:"omitempty"`
	DeviceName        string   `json:"device_name" binding:"omitempty,max=64"`
}
