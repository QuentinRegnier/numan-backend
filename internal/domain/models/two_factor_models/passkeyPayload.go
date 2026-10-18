package two_factor_models

type PasskeyPayload struct {
	ID              int64    `bson:"id" json:"id" msgpack:"id"`
	UserID          int64    `bson:"user_id" json:"user_id" msgpack:"user_id"`
	CredentialID    string   `bson:"credential_id" json:"credential_id" msgpack:"credential_id"`
	PublicKey       []byte   `bson:"public_key" json:"public_key" msgpack:"public_key"`
	AttestationType string   `bson:"attestation_type" json:"attestation_type" msgpack:"attestation_type"`
	AAGUID          []byte   `bson:"aaguid" json:"aaguid" msgpack:"aaguid"`
	SignCount       int64    `bson:"sign_count" json:"sign_count" msgpack:"sign_count"`
	DeviceName      string   `bson:"device_name" json:"device_name" msgpack:"device_name"`
	Transports      []string `bson:"transports" json:"transports" msgpack:"transports"`
	CreatedAt       int64    `bson:"created_at" json:"created_at" msgpack:"created_at"`
	UpdatedAt       int64    `bson:"updated_at" json:"updated_at" msgpack:"updated_at"`
}
