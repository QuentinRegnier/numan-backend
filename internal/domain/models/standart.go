package models

import (
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

type ExternalLinks struct {
	Title string `json:"title" bson:"title" msgpack:"title"`
	URL   string `json:"url" bson:"url" msgpack:"url"`
}

// Validate vérifie la cohérence d'un lien externe optionnel.
func (p *ExternalLinks) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le lien externe est nul.", nil)
	}
	title := strings.TrimSpace(p.Title)
	rawURL := strings.TrimSpace(p.URL)
	if title == "" && rawURL == "" {
		return nil
	}
	if utf8.RuneCountInString(title) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le titre du lien externe ne peut pas dépasser 100 caractères.", nil)
	}
	if rawURL == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'URL du lien externe est obligatoire lorsqu'un titre est fourni.", nil)
	}
	if utf8.RuneCountInString(rawURL) > 2048 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'URL du lien externe ne peut pas dépasser 2048 caractères.", nil)
	}
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'URL du lien externe est invalide.", nil)
	}
	return nil
}
