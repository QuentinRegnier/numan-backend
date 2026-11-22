package conversation_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// CreateCommunityInput valide les données pour la création d'une communauté publique (Type 3).
type CreateCommunityInput struct {
	Title        string               `json:"title" binding:"required,min=3,max=100"`
	Lang         string               `json:"lang" binding:"required"`
	Settings     ConversationSettings `json:"settings"`
	ExternalLink models.ExternalLinks `json:"external_link" binding:"omitempty"` // ✅ NOUVEAU
	OwnerID      int64                `json:"owner_id" binding:"omitempty"`
}

// Validate vérifie les contraintes métier de création d'une communauté publique.
func (p *CreateCommunityInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de création de communauté est nul.", nil)
	}
	title := strings.TrimSpace(p.Title)
	runeCount := utf8.RuneCountInString(title)
	if runeCount < 3 || runeCount > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le titre de la communauté doit contenir entre 3 et 100 caractères.", nil)
	}
	if _, ok := variables.NormalizeCommunityLang(p.Lang); !ok {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La langue de la communauté est obligatoire et doit faire partie des langues supportées.", nil)
	}
	if p.OwnerID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du propriétaire ne peut pas être négatif.", nil)
	}
	if err := p.Settings.Validate(); err != nil {
		return err
	}
	if err := p.ExternalLink.Validate(); err != nil {
		return err
	}
	return nil
}

// CreateCommunityOutput renvoie l'ID généré.
type CreateCommunityOutput struct {
	ConversationID int64 `json:"conversation_id"`
	InboxUpdateAt  int64 `json:"conversation_update_at"`
}
