package lite_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type ConversationSettings struct {
	WritePermission      int   `bson:"write_permission" json:"write_permission" msgpack:"write_permission"`
	JoinWithLinkDuration int64 `json:"join_with_link_duration" bson:"join_with_link_duration" msgpack:"join_with_link_duration"`
	JoinApprovalRequired bool  `bson:"join_approval_required" json:"join_approval_required" msgpack:"join_approval_required"`
	SendMediaPermission  bool  `bson:"send_media_permission" json:"send_media_permission" msgpack:"send_media_permission"`
	AddMemberPermission  bool  `bson:"add_member_permission" json:"add_member_permission" msgpack:"add_member_permission"`
	HideSystemMessages   bool  `bson:"hide_system_messages" json:"hide_system_messages" msgpack:"hide_system_messages"`
	SendSurveyPermission bool  `json:"send_survey_permission" bson:"send_survey_permission" msgpack:"send_survey_permission"`
}

// Validate vérifie les invariants métier des paramètres allégés de conversation.
func (p *ConversationSettings) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les paramètres de conversation sont nuls.", nil)
	}
	if p.WritePermission < 0 || p.WritePermission > 2 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La permission d'écriture doit être comprise entre 0 et 2.", nil)
	}
	if p.JoinWithLinkDuration < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La durée du lien d'invitation ne peut pas être négative.", nil)
	}
	return nil
}

type ConvLiteRequest struct {
	ID            int64                `bson:"id" json:"id"`
	Type          int                  `bson:"type" json:"type"`
	Title         string               `bson:"title" json:"title"`
	Description   string               `bson:"description" json:"description"`
	AvatarID      int64                `bson:"avatar_id" json:"avatar_id"`
	LastMessageID int64                `bson:"last_message_id" json:"last_message_id"`
	Settings      ConversationSettings `bson:"settings" json:"settings"` // Remplacement de Laws
	ExternalLink  models.ExternalLinks `bson:"external_links" json:"external_links"`
	Lang          string               `bson:"lang,omitempty" json:"lang,omitempty"`
	IsPartner     bool                 `bson:"is_partner" json:"is_partner"`
	PinSlot       int                  `bson:"pin_slot,omitempty" json:"pin_slot,omitempty"`
	Mode          string               `bson:"mode,omitempty" json:"mode,omitempty"`
}

// ResolveCommunityMode dérive le mode d'interaction canonique d'une communauté publique
// à partir de sa représentation en cache ConvMeta (ConvLiteRequest).
func ResolveCommunityMode(meta ConvLiteRequest) models.CommunityMode {
	return models.ResolveCommunityModeFromWritePermission(meta.Settings.WritePermission)
}

// Validate vérifie les invariants métier d'une conversation allégée en cache.
func (p *ConvLiteRequest) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload allégé de conversation est nul.", nil)
	}
	if p.ID <= 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de conversation doit être strictement positif.", nil)
	}
	if p.Type < variables.ConversationTypeDirect || p.Type > variables.ConversationTypeCommunityPub {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le type de conversation est invalide.", nil)
	}
	if utf8.RuneCountInString(strings.TrimSpace(p.Title)) > 100 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le titre de la conversation ne peut pas dépasser 100 caractères.", nil)
	}
	if utf8.RuneCountInString(p.Description) > 500 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La description de la conversation ne peut pas dépasser 500 caractères.", nil)
	}
	if p.AvatarID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant d'avatar ne peut pas être négatif.", nil)
	}
	if p.LastMessageID < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant du dernier message ne peut pas être négatif.", nil)
	}
	if strings.TrimSpace(p.Lang) != "" && !variables.IsSupportedCommunityLang(p.Lang) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La langue de la conversation n'est pas supportée.", nil)
	}
	if p.PinSlot != 0 && (p.PinSlot < variables.CommunityRecoMinPinSlot || p.PinSlot > variables.CommunityRecoMaxPinSlot) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le slot d'épinglage doit être compris entre 1 et 100.", nil)
	}
	if p.Mode != "" && !models.CommunityMode(p.Mode).IsValid() {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le mode d'interaction de la communauté est invalide.", nil)
	}
	if err := p.Settings.Validate(); err != nil {
		return err
	}
	if err := p.ExternalLink.Validate(); err != nil {
		return err
	}
	return nil
}
