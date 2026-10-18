package auth_models

// PreAuthSession est stockée uniquement dans Redis L1 (TTL 5 minutes).
type PreAuthSession struct {
	Token             string      `json:"token" msgpack:"token"`
	Flow              string      `json:"flow" msgpack:"flow"` // "login" | "signup" | "setup"
	UserID            int64       `json:"user_id" msgpack:"user_id"`
	Method            int         `json:"method" msgpack:"method"`
	AvailableMethods  []int       `json:"available_methods,omitempty" msgpack:"available_methods"`
	CodeHash          string      `json:"code_hash,omitempty" msgpack:"code_hash"`
	TempTOTPSecret    string      `json:"temp_totp_secret,omitempty" msgpack:"temp_totp_secret"`
	WebAuthnChallenge string      `json:"webauthn_challenge,omitempty" msgpack:"webauthn_challenge"`
	DeviceName        string      `json:"device_name,omitempty" msgpack:"device_name"`
	LoginData         LoginInput  `json:"login_data,omitempty" msgpack:"login_data"`
	SignUpData        SignUpInput `json:"signup_data,omitempty" msgpack:"signup_data"`
	IPAddresses       []string    `json:"ip_addresses" msgpack:"ip_addresses"`
	Attempts          int         `json:"attempts" msgpack:"attempts"`
	CreatedAt         int64       `json:"created_at" msgpack:"created_at"`
}
