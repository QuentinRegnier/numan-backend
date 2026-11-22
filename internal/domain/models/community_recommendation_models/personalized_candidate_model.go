package community_recommendation_models

// PersonalizedCandidate représente une communauté après boost amis et/ou injection de pin.
// PinSlot vaut 0 si non épinglée, 1..100 sinon.
type PersonalizedCandidate struct {
	ConversationID    int64
	GlobalScore       float64
	PersonalizedScore float64
	FriendCount       int
	IsPinned          bool
	PinSlot           int
}
