package two_factor_models

type PasskeyAssertionResponse struct {
	CredentialID      string `json:"credential_id" binding:"required"`
	AuthenticatorData string `json:"authenticator_data" binding:"required"`
	ClientDataJSON    string `json:"client_data_json" binding:"required"`
	Signature         string `json:"signature" binding:"required"`
	UserHandle        string `json:"user_handle,omitempty"`
}
