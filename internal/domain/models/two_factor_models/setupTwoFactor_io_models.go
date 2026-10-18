package two_factor_models

type SetupTwoFactorInput struct {
	Method     int    `json:"method" binding:"required,oneof=1 2 3"`
	DeviceName string `json:"device_name" binding:"omitempty,max=64"`
}

type SetupTwoFactorOutput struct {
	SetupToken     string `json:"setup_token"`
	Method         int    `json:"method"`
	TOTPSecret     string `json:"totp_secret,omitempty"`
	TOTPQRCodeURI  string `json:"totp_qr_uri,omitempty"`
	PasskeyOptions any    `json:"passkey_options,omitempty"`
	Message        string `json:"message"`
}
