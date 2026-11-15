package conversation_models

import (
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// SuggestInput unifie la recherche de contacts. L'intent permet d'évaluer la bonne permission.
type SuggestInput struct {
	ConversationID int64  `json:"conversation_id"` // 0 = Nouveau MP, > 0 = Groupe existant
	Query          string `json:"query" binding:"omitempty,max=50"`
	Limit          int64  `json:"limit" binding:"omitempty,min=1,max=50"`
	Offset         int64  `json:"offset" binding:"omitempty,min=0"`
	Intent         string `json:"intent" binding:"omitempty,oneof=dm group tag mention"` // ✅ NOUVEAU
}

// Validate vérifie les paramètres de suggestion de contacts.
func (p *SuggestInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de suggestion est nul.", nil)
	}
	if p.ConversationID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation ne peut pas être négatif.", nil)
	}
	if utf8.RuneCountInString(p.Query) > 50 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de recherche ne peut pas dépasser 50 caractères.", nil)
	}
	if p.Limit < 0 || p.Limit > 50 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La limite doit être comprise entre 1 et 50.", nil)
	}
	if p.Offset < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'offset ne peut pas être négatif.", nil)
	}
	switch p.Intent {
	case "", "dm", "group", "tag", "mention":
	default:
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'intention de suggestion est invalide.", nil)
	}
	return nil
}

type SuggestOutput struct {
	Users []auth_models.UserLiteView `json:"users"`
}
