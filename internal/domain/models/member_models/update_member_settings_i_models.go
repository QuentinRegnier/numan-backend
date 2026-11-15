package member_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"

// UpdateMemberSettingsInput valide le payload complet envoyé par l'application
type UpdateMemberSettingsInput struct {
	ConversationID    int64 `json:"conversation_id" binding:"required"`
	IsMuted           int   `json:"is_muted" binding:"min=0,max=2"`
	MuteExpiresAt     int64 `json:"mute_expires_at"`
	MediaAutoDownload bool  `json:"media_auto_download"`
}

// Validate vérifie les contraintes métier des paramètres personnels d'un membre.
func (p *UpdateMemberSettingsInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de mise à jour des paramètres est nul.", nil)
	}
	if p.ConversationID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if p.IsMuted < 0 || p.IsMuted > 2 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le mode silencieux doit être compris entre 0 et 2.", nil)
	}
	if p.MuteExpiresAt < -1 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La date d'expiration du mode silencieux est invalide.", nil)
	}
	return nil
}

type UpdateMemberSettingsOutput struct {
	InboxUpdateAt int64 `json:"inbox_update_at"`
}
