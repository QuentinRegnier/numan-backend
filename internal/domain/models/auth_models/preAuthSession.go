package auth_models

import (
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

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

// Validate vérifie la cohérence interne d'une session de pré-authentification 2FA.
func (p *PreAuthSession) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La session de pré-authentification est nulle.", nil)
	}
	if strings.TrimSpace(p.Token) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le jeton de pré-authentification est obligatoire.", nil)
	}
	switch p.Flow {
	case "login", "setup":
		if p.UserID <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur est requis pour ce flux 2FA.", nil)
		}
	case "signup":
		if p.UserID < 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant utilisateur ne peut pas être négatif.", nil)
		}
	default:
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le flux de pré-authentification 2FA est invalide.", nil)
	}
	if p.Method < 0 || p.Method > (variables.TwoFactorBitEmail|variables.TwoFactorBitTOTP|variables.TwoFactorBitPasskey) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le masque de méthodes 2FA est invalide.", nil)
	}
	for _, m := range p.AvailableMethods {
		switch m {
		case variables.TwoFactorMethodEmail, variables.TwoFactorMethodTOTP, variables.TwoFactorMethodPasskey:
		default:
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Une méthode 2FA disponible est invalide.", nil)
		}
	}
	if p.Attempts < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le compteur de tentatives ne peut pas être négatif.", nil)
	}
	return nil
}
