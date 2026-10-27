package auth_models

type TrustDeltaResult struct {
	NewScore float64
	IsBanned bool
}

type MonthlyTrustReward struct {
	UserID        int64
	NewTrustScore float64
}
