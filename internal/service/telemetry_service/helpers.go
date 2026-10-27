package telemetry_service

import "github.com/QuentinRegnier/numan-backend/internal/variables"

// ComputeTrustTier retourne le palier de confiance (0=Banni, 1=Quarantaine, 2=Standard, 3=VIP).
func computeTrustTier(score float64) int {
	switch {
	case score <= variables.TrustScoreMin:
		return variables.TrustTierBanned // Au lieu de 0
	case score < variables.TrustThresholdStandard:
		return variables.TrustTierQuarantine // Au lieu de 1
	case score < variables.TrustThresholdVIP:
		return variables.TrustTierStandard // Au lieu de 2
	default:
		return variables.TrustTierVIP // Au lieu de 3
	}
}
