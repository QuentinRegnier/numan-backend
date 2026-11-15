package lite_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// CommunityLiteRequest représente les données compressées stockées dans le Speed Cache Redis (MsgPack).
type CommunityLiteRequest struct {
	ID               int64  `msgpack:"id"`
	Name             string `msgpack:"name"`
	ProfilePictureID int64  `msgpack:"profile_picture_id"`
	Description      string `msgpack:"description"`
	MemberCount      int    `msgpack:"member_count"`
}

// Validate vérifie les invariants métier d'une communauté allégée en cache.
func (p *CommunityLiteRequest) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload allégé de communauté est nul.", nil)
	}
	if p.ID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de communauté doit être strictement positif.", nil)
	}
	trimmedName := strings.TrimSpace(p.Name)
	if trimmedName == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom de la communauté est obligatoire.", nil)
	}
	if utf8.RuneCountInString(trimmedName) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom de la communauté ne peut pas dépasser 100 caractères.", nil)
	}
	if utf8.RuneCountInString(p.Description) > 500 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La description de la communauté ne peut pas dépasser 500 caractères.", nil)
	}
	if p.ProfilePictureID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de photo de profil ne peut pas être négatif.", nil)
	}
	if p.MemberCount < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nombre de membres ne peut pas être négatif.", nil)
	}
	return nil
}
