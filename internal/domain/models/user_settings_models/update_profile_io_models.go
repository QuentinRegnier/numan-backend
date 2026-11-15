package user_settings_models

import (
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// UpdateProfileInput définit les champs publics modifiables (Remplacement intégral PUT).
type UpdateProfileInput struct {
	Username         string `json:"username" binding:"required,min=3,max=30,alphanum"`
	Email            string `json:"email" binding:"required,email,max=100"`
	Phone            string `json:"phone" binding:"omitempty,e164"` // omitempty car un format E164 vide crasherait
	FirstName        string `json:"first_name" binding:"required,max=50"`
	LastName         string `json:"last_name" binding:"required,max=50"`
	Bio              string `json:"bio" binding:"max=500"` // Sans omitempty : "" est valide
	Location         string `json:"location" binding:"max=100"`
	School           string `json:"school" binding:"max=100"`
	Work             string `json:"work" binding:"max=100"`
	ProfilePictureID int64  `json:"profile_picture_id" binding:"omitempty"`
}

// Validate vérifie l'ensemble des contraintes de format et de longueur du profil utilisateur.
func (p *UpdateProfileInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload est nul.", nil)
	}
	if !isValidUsername(p.Username) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom d'utilisateur doit contenir entre 3 et 30 caractères alphanumériques ASCII.", nil)
	}
	if !isValidEmail(p.Email) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'adresse email est invalide.", nil)
	}
	if p.Phone != "" && !isValidE164(p.Phone) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le numéro de téléphone doit respecter le format E.164 (ex: +33612345678).", nil)
	}
	trimmedFirst := strings.TrimSpace(p.FirstName)
	if trimmedFirst == "" || utf8.RuneCountInString(p.FirstName) > 50 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le prénom est requis et ne peut pas dépasser 50 caractères.", nil)
	}
	trimmedLast := strings.TrimSpace(p.LastName)
	if trimmedLast == "" || utf8.RuneCountInString(p.LastName) > 50 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom est requis et ne peut pas dépasser 50 caractères.", nil)
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
	if p.ProfilePictureID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la photo de profil ne peut pas être négatif.", nil)
	}
	return nil
}

func isValidUsername(u string) bool {
	if len(u) < 3 || len(u) > 30 {
		return false
	}
	for i := 0; i < len(u); i++ {
		c := u[i]
		isDigit := c >= '0' && c <= '9'
		isLower := c >= 'a' && c <= 'z'
		isUpper := c >= 'A' && c <= 'Z'
		if !isDigit && !isLower && !isUpper {
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
	if len(parts) != 2 || parts[0] == "" || !strings.Contains(parts[1], ".") || strings.HasPrefix(parts[1], ".") || strings.HasSuffix(parts[1], ".") {
		return false
	}
	return true
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

type UpdateProfileOutput struct {
	ProfileUpdatedAt int64 `json:"profile_updated_at"`
}
