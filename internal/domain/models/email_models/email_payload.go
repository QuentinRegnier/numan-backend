package email_models

import (
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// EmailPayload représente le paquet de données standardisé pour l'expédition d'un email.
// Il transite via le Cerveau Asynchrone (Redis) pour éviter de bloquer l'API HTTP.
type EmailPayload struct {
	RecipientEmail string         `json:"recipient_email" binding:"required,email"`
	TemplateName   string         `json:"template_name" binding:"required"`
	Subject        string         `json:"subject" binding:"required"`
	Data           map[string]any `json:"data"` // Contient les variables dynamiques (ex: "Username")
}

// Validate vérifie l'adresse destinataire, l'intégrité du nom de template et l'absence d'injection SMTP dans le sujet.
func (p *EmailPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload email est nul.", nil)
	}
	recipient := strings.TrimSpace(p.RecipientEmail)
	if recipient == "" || len(recipient) > 254 || strings.ContainsAny(recipient, "\r\n") {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'adresse email du destinataire est invalide.", nil)
	}
	parsed, err := mail.ParseAddress(recipient)
	if err != nil || parsed.Address != recipient || !strings.Contains(parsed.Address, "@") {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le format de l'adresse email du destinataire est invalide.", nil)
	}

	tpl := strings.TrimSpace(p.TemplateName)
	tplLen := utf8.RuneCountInString(tpl)
	if tplLen == 0 || tplLen > 128 || strings.ContainsAny(tpl, "/\\\r\n") || strings.Contains(tpl, "..") {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom du template email est invalide.", nil)
	}

	if strings.ContainsAny(p.Subject, "\r\n") {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le sujet de l'email contient des caractères interdits.", nil)
	}
	subjLen := utf8.RuneCountInString(strings.TrimSpace(p.Subject))
	if subjLen == 0 || subjLen > 255 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le sujet de l'email doit contenir entre 1 et 255 caractères.", nil)
	}
	return nil
}
