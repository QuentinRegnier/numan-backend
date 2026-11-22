package community_recommendation_models

import "github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"

// ActiveCommunityRecoRecord représente une communauté publique active chargée depuis PostgreSQL
// pour le recalcul périodique du classement global par langue.
// PinSlot vaut 0 lorsqu'elle n'est pas épinglée, et 1..100 sinon.
type ActiveCommunityRecoRecord struct {
	ID          int64
	Title       string
	Description string
	AvatarID    int64
	Lang        string
	IsPartner   bool
	PinSlot     int
	Settings    conversation_models.ConversationSettings
	CreatedAt   int64
	MemberCount int
}
