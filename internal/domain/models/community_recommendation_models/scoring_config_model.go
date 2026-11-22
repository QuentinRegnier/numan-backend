package community_recommendation_models

// ScoringConfig regroupe tous les hyperparamètres du moteur de scoring communautaire.
type ScoringConfig struct {
	WeightMembers        float64
	WeightDynamism       float64
	WeightGrowth         float64
	AlphaOwner           float64
	AlphaAdmin           float64
	AlphaMember          float64
	TrustPriorMean       float64
	TrustPriorStrength   float64
	TrustMemberSampleCap int
	ModeMultFreeChat     float64
	ModeMultSubreddit    float64
	ModeMultAnnouncement float64
	PartnerMultiplier    float64
	FreshnessBoost       float64
	FreshnessDays        int
	FriendGamma          float64
}
