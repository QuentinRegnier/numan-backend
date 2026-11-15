package message_models

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// ReactMessageInput valide l'alias de la réaction.
type ReactMessageInput struct {
	MessageID int64  `json:"message_id" binding:"required"`
	Reaction  string `json:"reaction" binding:"required,min=1,max=30,print"` // ex: "heart" - Rejette les chaînes vides
}

// Validate vérifie les règles métier d'ajout d'une réaction sur un message.
func (p *ReactMessageInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de réaction est nul.", nil)
	}
	if p.MessageID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du message doit être strictement positif.", nil)
	}
	reaction := strings.TrimSpace(p.Reaction)
	runeCount := utf8.RuneCountInString(reaction)
	if runeCount < 1 || runeCount > 30 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La réaction doit contenir entre 1 et 30 caractères.", nil)
	}
	for _, r := range reaction {
		if !unicode.IsPrint(r) {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La réaction contient des caractères non imprimables.", nil)
		}
	}
	return nil
}
