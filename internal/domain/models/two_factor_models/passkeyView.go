package two_factor_models

type PasskeyView struct {
	ID           int64    `json:"id"`
	CredentialID string   `json:"credential_id"`
	DeviceName   string   `json:"device_name"`
	Transports   []string `json:"transports"`
	CreatedAt    int64    `json:"created_at"`
	UpdatedAt    int64    `bson:"updated_at" json:"updated_at"`
}
