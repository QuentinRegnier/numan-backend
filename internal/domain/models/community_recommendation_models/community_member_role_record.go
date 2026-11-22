package community_recommendation_models

// CommunityMemberRoleRecord représente le couple (conversation_id, user_id, role)
// utilisé pour le calcul du score de confiance hiérarchique d'une communauté.
type CommunityMemberRoleRecord struct {
	ConversationID int64
	UserID         int64
	Role           int
}
