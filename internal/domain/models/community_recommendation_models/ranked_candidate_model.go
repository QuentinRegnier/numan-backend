package community_recommendation_models

// RankedCandidate représente une communauté issue du ZSET global précalculé.
type RankedCandidate struct {
	ConversationID int64
	GlobalScore    float64
}
