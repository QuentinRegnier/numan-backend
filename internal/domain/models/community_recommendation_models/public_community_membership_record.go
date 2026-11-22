package community_recommendation_models

// PublicCommunityMembershipRecord représente une appartenance active (user_id, conversation_id)
// utilisée lors du seeding à froid de user:public_comms:{user_id}.
type PublicCommunityMembershipRecord struct {
	UserID         int64
	ConversationID int64
}
