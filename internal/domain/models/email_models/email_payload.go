package email_models

// EmailPayload représente le paquet de données standardisé pour l'expédition d'un email.
// Il transite via le Cerveau Asynchrone (Redis) pour éviter de bloquer l'API HTTP.
type EmailPayload struct {
	RecipientEmail string         `json:"recipient_email" binding:"required,email"`
	TemplateName   string         `json:"template_name" binding:"required"`
	Subject        string         `json:"subject" binding:"required"`
	Data           map[string]any `json:"data"` // Contient les variables dynamiques (ex: "Username")
}
