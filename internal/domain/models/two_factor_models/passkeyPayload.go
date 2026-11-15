package two_factor_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

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

// Validate vérifie les invariants métier d'une passkey persistée.
func (p *PasskeyPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de la passkey est nul.", nil)
	}
	if p.ID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la passkey doit être strictement positif.", nil)
	}
	if p.UserID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur doit être strictement positif.", nil)
	}
	if strings.TrimSpace(p.CredentialID) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant WebAuthn (credential_id) est obligatoire.", nil)
	}
	if len(p.PublicKey) == 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La clé publique de la passkey est obligatoire.", nil)
	}
	if p.SignCount < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le compteur de signatures ne peut pas être négatif.", nil)
	}
	if utf8.RuneCountInString(p.DeviceName) > 64 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom de l'appareil ne peut pas dépasser 64 caractères.", nil)
	}
	return nil
}
