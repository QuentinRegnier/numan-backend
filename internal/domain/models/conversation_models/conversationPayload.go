package conversation_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

type ConversationSettings struct {
	WritePermission      int   `json:"write_permission" bson:"write_permission" msgpack:"write_permission"`
	JoinWithLinkDuration int64 `json:"join_with_link_duration" bson:"join_with_link_duration" msgpack:"join_with_link_duration"`
	JoinApprovalRequired bool  `json:"join_approval_required" bson:"join_approval_required" msgpack:"join_approval_required"`
	SendMediaPermission  bool  `json:"send_media_permission" bson:"send_media_permission" msgpack:"send_media_permission"`
	AddMemberPermission  bool  `json:"add_member_permission" bson:"add_member_permission" msgpack:"add_member_permission"`
	HideSystemMessages   bool  `json:"hide_system_messages" bson:"hide_system_messages" msgpack:"hide_system_messages"`
	SendSurveyPermission bool  `json:"send_survey_permission" bson:"send_survey_permission" msgpack:"send_survey_permission"`
}

// Validate vérifie les invariants métier des paramètres d'une conversation.
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

// ConversationPayload correspond exactement au schéma Postgres messaging.conversations
type ConversationPayload struct {
	ID            int64                `json:"id" bson:"id"`
	Type          int                  `json:"type" bson:"type"` // 0, 1, 2, 3
	Title         string               `json:"title" bson:"title"`
	Description   string               `json:"description" bson:"description"`
	AvatarID      int64                `json:"avatar_id" bson:"avatar_id"`
	LastMessageID int64                `json:"last_message_id" bson:"last_message_id"`
	State         int                  `json:"state" bson:"state"`       // -2, -1, 0=active, 1=archived
	Settings      ConversationSettings `json:"settings" bson:"settings"` // Remplacement de Laws []int
	ExternalLink  models.ExternalLinks `json:"external_link" bson:"external_link"`
	Lang          string               `json:"lang,omitempty" bson:"lang,omitempty"`
	IsPartner     bool                 `json:"is_partner" bson:"is_partner"`
	PinSlot       int                  `json:"pin_slot,omitempty" bson:"pin_slot,omitempty"`
	CreatedAt     int64                `json:"created_at" bson:"created_at"`
	UpdatedAt     int64                `json:"updated_at" bson:"updated_at"`
}

// ResolveCommunityMode dérive le mode d'interaction canonique à partir des settings de la conversation.
func (p *ConversationPayload) ResolveCommunityMode() models.CommunityMode {
	if p == nil {
		return models.CommunityModeSubreddit
	}
	return models.ResolveCommunityModeFromWritePermission(p.Settings.WritePermission)
}

// Validate vérifie l'intégrité complète d'un ConversationPayload avant mise en cache ou persistance.
func (p *ConversationPayload) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le payload de conversation est nul.", nil)
	}
	if p.Type < variables.ConversationTypeDirect || p.Type > variables.ConversationTypeCommunityPub {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le type de conversation est invalide.", nil)
	}
	if p.State < variables.ConversationStateGroupDelete || p.State > variables.ConversationStateArchived {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'état de la conversation est invalide.", nil)
	}
	title := strings.TrimSpace(p.Title)
	if p.Type != variables.ConversationTypeDirect && p.State == variables.ConversationStateActive && title == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Un groupe ou une communauté requiert un titre valide.", nil)
	}
	if utf8.RuneCountInString(title) > 100 {
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
	if p.Type != variables.ConversationTypeCommunityPub {
		if strings.TrimSpace(p.Lang) != "" {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La langue n'est autorisée que pour les communautés publiques.", nil)
		}
		if p.PinSlot != 0 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le slot d'épinglage n'est autorisé que pour les communautés publiques.", nil)
		}
		if p.IsPartner {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le statut partenaire n'est autorisé que pour les communautés publiques.", nil)
		}
	} else {
		if strings.TrimSpace(p.Lang) != "" && !variables.IsSupportedCommunityLang(p.Lang) {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La langue de la communauté n'est pas supportée.", nil)
		}
		if p.PinSlot != 0 && (p.PinSlot < variables.CommunityRecoMinPinSlot || p.PinSlot > variables.CommunityRecoMaxPinSlot) {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le slot d'épinglage doit être compris entre 1 et 100.", nil)
		}
	}
	if err := p.Settings.Validate(); err != nil {
		return err
	}
	if err := p.ExternalLink.Validate(); err != nil {
		return err
	}
	return nil
}

func DefaultConversationSettings(convType int) ConversationSettings {
	switch convType {
	case 3: // Communautés (Vitrine par défaut)
		return ConversationSettings{
			JoinApprovalRequired: false,
			WritePermission:      2,     // Annonces & Threads
			SendMediaPermission:  false, // Bloque les médias aux membres
			AddMemberPermission:  false, // Bloque l'ajout aux membres
			HideSystemMessages:   true,  // On cache le bruit de fond
		}
	default: // MP (Type 0) ou inconnu
		return ConversationSettings{
			JoinApprovalRequired: false,
			WritePermission:      0,
			SendMediaPermission:  true,
			AddMemberPermission:  true,
			HideSystemMessages:   false,
		}
	}
}
