package community_recommendation_models

// CommunityPinRecord représente une communauté publique activement épinglée
// sur un slot fixe (1..100) dans sa langue.
type CommunityPinRecord struct {
	ConversationID int64
	Lang           string
	PinSlot        int
}
