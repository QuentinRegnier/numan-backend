package post_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type UpdatePostInput struct {
	PostID      int64    `json:"post_id" binding:"required"`
	Content     string   `json:"content" binding:"max=2200"`
	Hashtags    []string `json:"hashtags" binding:"max=10,dive,alphanum,max=50"`
	Identifiers []int64  `json:"identifiers" binding:"max=10"`
	Location    string   `json:"location" binding:"max=100"`
	Visibility  int      `json:"visibility" binding:"oneof=0 1 2"` // 0: Public, 1: Abonnés, 2: Amis (Mode 3 Privé supprimé)
	Lang        *int     `json:"lang,omitempty"`
	LangISO     string   `json:"lang_iso,omitempty"`
}

// Validate vérifie les règles métier de mise à jour d'une publication.
func (p *UpdatePostInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de mise à jour de publication est nul.", nil)
	}
	if p.PostID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de la publication doit être strictement positif.", nil)
	}
	if utf8.RuneCountInString(p.Content) > 2200 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le contenu de la publication ne peut pas dépasser 2200 caractères.", nil)
	}
	if utf8.RuneCountInString(p.Location) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La localisation ne peut pas dépasser 100 caractères.", nil)
	}

	uniqueHashtags := make(map[string]struct{}, len(p.Hashtags))
	for _, h := range p.Hashtags {
		trimmed := strings.TrimSpace(h)
		if trimmed == "" {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Un hashtag ne peut pas être vide.", nil)
		}
		if utf8.RuneCountInString(trimmed) > 50 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Un hashtag ne peut pas dépasser 50 caractères.", nil)
		}
		uniqueHashtags[trimmed] = struct{}{}
	}
	if len(uniqueHashtags) > variables.MaxHashtagsPerPost {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nombre de hashtags dépasse la limite autorisée.", nil)
	}

	uniqueIdentifiers := make(map[int64]struct{}, len(p.Identifiers))
	for _, id := range p.Identifiers {
		if id <= 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Tous les identifiants taggés doivent être strictement positifs.", nil)
		}
		uniqueIdentifiers[id] = struct{}{}
	}
	if len(uniqueIdentifiers) > variables.MaxIdentifiersPerPost {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nombre d'identifiants dépasse la limite autorisée.", nil)
	}

	if p.Visibility < variables.PostVisibilityAll || p.Visibility > variables.PostVisibilityFriend {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Niveau de visibilité non supporté.", nil)
	}
	if p.Lang != nil && !variables.IsSupportedLanguage(*p.Lang) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La langue spécifiée pour la publication n'est pas supportée.", nil)
	}
	if strings.TrimSpace(p.LangISO) != "" {
		code, err := variables.ParseLanguageISO(p.LangISO)
		if err != nil || !variables.IsSupportedLanguage(code) {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le code ISO de langue spécifié pour la publication n'est pas supporté.", err)
		}
	}
	return nil
}
