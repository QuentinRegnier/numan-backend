package community_recommendation_models

// GlobalScoreBreakdown détaille chaque facteur du score global (utile pour tests & audit).
type GlobalScoreBreakdown struct {
	MembersScore        float64
	DynamismScore       float64
	GrowthScore         float64
	BaseScore           float64
	CommunityTrust      float64
	TrustMultiplier     float64
	ModeMultiplier      float64
	PartnerMultiplier   float64
	FreshnessMultiplier float64
	GlobalScore         float64
}
