package auth_models

import (
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type SignUpInput struct {
	Username               string                                         `json:"username" binding:"required,min=3,max=30,alphanum" example:"johndoe"`
	Email                  string                                         `json:"email" binding:"required,email,max=100" example:"john@numan.com"`
	Phone                  string                                         `json:"phone" binding:"required,e164" example:"+33612345678"`
	PasswordHash           string                                         `json:"password_hash" binding:"required,min=8" example:"secretPass123"`
	FirstName              string                                         `json:"first_name" binding:"max=50" example:"John"`
	LastName               string                                         `json:"last_name" binding:"max=50" example:"Doe"`
	Birthdate              string                                         `json:"birthdate" binding:"required,len=8,numeric" example:"25121990"`
	Gender                 int                                            `json:"gender" binding:"omitempty,oneof=0 1 2" example:"1"`
	Bio                    string                                         `json:"bio" binding:"max=500" example:"J'aime la tech"`
	Location               string                                         `json:"location" binding:"max=100" example:"Paris"`
	School                 string                                         `json:"school" binding:"max=100" example:"42"`
	Work                   string                                         `json:"work" binding:"max=100" example:"Developer"`
	DeviceInfo             map[string]interface{}                         `json:"device_info" swaggertype:"object"`
	FirebaseInstallationID string                                         `json:"firebase_installation_id" binding:"required" example:"eyJhbGciOiJIUzI1Ni..."`
	DisplayAndContent      user_settings_models.DisplayAndContentSettings `json:"display_and_content" binding:"omitempty"`
	Privacy                user_settings_models.PrivacySettings           `json:"privacy" binding:"omitempty"`
	Notifications          user_settings_models.NotificationSettings      `json:"notifications" binding:"omitempty"`
	ProfilePictureID       int64                                          `json:"profile_picture_id" binding:"omitempty"`
	TwoFactorMethod        int                                            `json:"two_factor_method" binding:"omitempty,oneof=0 1 2 3"` // 0: Aucun, 1: Email, 2: TOTP, 3: Passkey
}

// Validate vérifie l'ensemble des invariants d'inscription utilisateur.
func (p *SignUpInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload d'inscription est nul.", nil)
	}
	if !isValidUsername(p.Username) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom d'utilisateur doit contenir entre 3 et 30 caractères alphanumériques ASCII.", nil)
	}
	if !isValidEmail(p.Email) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'adresse email est invalide.", nil)
	}
	if !isValidE164(p.Phone) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le numéro de téléphone doit respecter le format E.164.", nil)
	}
	if strings.TrimSpace(p.PasswordHash) == "" || len(p.PasswordHash) < 8 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le mot de passe doit contenir au moins 8 caractères.", nil)
	}
	if utf8.RuneCountInString(p.FirstName) > 50 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le prénom ne peut pas dépasser 50 caractères.", nil)
	}
	if utf8.RuneCountInString(p.LastName) > 50 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom ne peut pas dépasser 50 caractères.", nil)
	}
	if !isEightDigits(p.Birthdate) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format de date de naissance invalide (attendu: JJMMAAAA).", nil)
	}
	parsedBirthdate, err := time.Parse("02012006", p.Birthdate)
	if err != nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Date de naissance invalide (attendu: JJMMAAAA).", err)
	}
	age := int(domain.NowTime().Sub(parsedBirthdate).Hours() / 24 / 365)
	if age < 13 || age > 120 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'âge doit être compris entre 13 et 120 ans.", nil)
	}
	if p.Gender < 0 || p.Gender > 2 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le genre sélectionné est invalide.", nil)
	}
	if utf8.RuneCountInString(p.Bio) > 500 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La biographie ne peut pas dépasser 500 caractères.", nil)
	}
	if utf8.RuneCountInString(p.Location) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La localisation ne peut pas dépasser 100 caractères.", nil)
	}
	if utf8.RuneCountInString(p.School) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'établissement scolaire ne peut pas dépasser 100 caractères.", nil)
	}
	if utf8.RuneCountInString(p.Work) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le poste/entreprise ne peut pas dépasser 100 caractères.", nil)
	}
	if strings.TrimSpace(p.FirebaseInstallationID) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant d'installation Firebase est obligatoire.", nil)
	}
	if err := p.DisplayAndContent.Validate(); err != nil {
		return err
	}
	if err := p.Privacy.Validate(); err != nil {
		return err
	}
	if err := p.Notifications.Validate(); err != nil {
		return err
	}
	if p.ProfilePictureID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la photo de profil ne peut pas être négatif.", nil)
	}
	switch p.TwoFactorMethod {
	case variables.TwoFactorMethodNone, variables.TwoFactorMethodEmail, variables.TwoFactorMethodTOTP, variables.TwoFactorMethodPasskey:
		return nil
	default:
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La méthode 2FA sélectionnée est invalide.", nil)
	}
}

func isValidUsername(u string) bool {
	if len(u) < 3 || len(u) > 30 {
		return false
	}
	for i := 0; i < len(u); i++ {
		c := u[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}

func isValidEmail(email string) bool {
	if email == "" || len(email) > 100 || strings.ContainsAny(email, " \t\r\n<>") {
		return false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return false
	}
	parts := strings.Split(email, "@")
	return len(parts) == 2 && parts[0] != "" && strings.Contains(parts[1], ".") && !strings.HasPrefix(parts[1], ".") && !strings.HasSuffix(parts[1], ".")
}

func isValidE164(phone string) bool {
	if len(phone) < 3 || len(phone) > 16 || phone[0] != '+' || phone[1] < '1' || phone[1] > '9' {
		return false
	}
	for i := 2; i < len(phone); i++ {
		if phone[i] < '0' || phone[i] > '9' {
			return false
		}
	}
	return true
}

func isEightDigits(s string) bool {
	if len(s) != 8 {
		return false
	}
	for i := 0; i < 8; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

type SignUpResponse struct {
	RequiresTwoFactor  bool                   `json:"requires_two_factor"`
	TwoFactorMethod    int                    `json:"two_factor_method,omitempty"`
	PreAuthToken       string                 `json:"pre_auth_token,omitempty"`
	TOTPSecret         string                 `json:"totp_secret,omitempty"`
	TOTPQRCodeURI      string                 `json:"totp_qr_uri,omitempty"`
	PasskeyOptions     any                    `json:"passkey_options,omitempty"`
	UserID             int64                  `json:"user_id,omitempty" example:"42"`
	MasterToken        string                 `json:"master_token,omitempty"`
	JWT                string                 `json:"jwt,omitempty"`
	ExpiresAt          int64                  `json:"expires_at,omitempty"`
	Message            string                 `json:"message" example:"User created successfully"`
	Avatar             media_models.MediaView `json:"avatar"`
	TrustScore         float64                `json:"trust_score"`
	TelemetryVector    []float32              `json:"telemetry_vector,omitempty"`
	TelemetryTopTags   []string               `json:"telemetry_top_tags,omitempty"`
	TelemetryTimestamp int64                  `json:"telemetry_timestamp,omitempty"`
}
