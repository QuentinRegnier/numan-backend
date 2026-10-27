package moderation_service

import (
	"math"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// MasterScoringInput regroupe l'ensemble des paramètres requis par l'Équation Maîtresse Unifiée (§C.2–C.8).
type MasterScoringInput struct {
	TargetType       int       // ReportTargetPost, ReportTargetComment, ReportTargetMessage, ReportTargetUser, ReportTargetConversation
	Origin           int       // ReportOriginUser, ReportOriginAutoAI, ReportOriginHybrid, ReportOriginSystem
	Category         int       // ReportCat* (6 et 9 déclenchent +Inf)
	IAScores         []float64 // Scores bruts NSFWJS par image (vide pour du texte pur)
	MaxIAScore       float64   // Score IA déjà agrégé si IAScores est vide
	HasImageOrPost   bool      // Vrai uniquement si la cible contient une image ou est une publication média (Scénarios A, A.2, B)
	IsPureText       bool      // Vrai pour le Scénario C (texte pur, commentaire, profil, salon) -> force P_IA=0 et B_jeunesse=0
	AuthorTrustScore float64   // T_auteur dans [0.0, 1.0]
	AccountAgeDays   float64   // Ancienneté du compte de l'auteur en jours (>= 0)
	ReporterTrusts   []float64 // Scores de confiance individuels T_sig,j des signaleurs humains
	MaxReporterOmega float64   // Optionnel : max(omega_j) précalculé en Redis
	SumReporterOmega float64   // Optionnel : sum(omega_j) précalculé en Redis
	EconomicValue    float64   // V_éco brut calculé par calculateEconomicImportance
	AgeHours         float64   // Delta t en heures depuis la création du ticket (>= 0)
}

// MasterScoringResult détaille chaque composante intermédiaire ainsi que le score final P_file.
type MasterScoringResult struct {
	IAScore          float64 // P_IA
	CrowdScore       float64 // S_sig
	AuthorShield     float64 // Phi_auteur
	EconomicBonus    float64 // B_éco
	YouthBonus       float64 // B_jeunesse
	BasePriority     float64 // P_base
	TargetWeight     float64 // W_cible
	OriginWeight     float64 // W_origine
	CategoryGravity  float64 // G_cat (peut valoir +Inf)
	StaticBaseScore  float64 // P_base * W_cible * W_origine * G_cat (avant M_temps, pour le Cron de vieillissement)
	AgingMultiplier  float64 // M_temps
	FinalQueueScore  float64 // P_file (peut valoir +Inf)
	IsInfiniteUrgent bool    // Vrai si G_cat == +Inf (Catégories 6 Mineur et 9 Automutilation/Suicide)
}

// ComputeCrowdTrustFromMaxAndSum calcule S_sig = max(omega_j) + gamma_log * ln(1 + sum(omega_j)).
// Retourne exactement 0.0 lorsqu'aucun signaleur humain n'est présent (pas de NaN).
func ComputeCrowdTrustFromMaxAndSum(maxOmega, sumOmega float64) float64 {
	if maxOmega <= 0 && sumOmega <= 0 {
		return 0.0
	}
	if maxOmega < 0 {
		maxOmega = 0
	}
	if sumOmega < 0 {
		sumOmega = 0
	}
	return maxOmega + variables.ReportLogDampingFactor*math.Log(1.0+sumOmega)
}

// ComputeCrowdTrustScore calcule S_sig à partir de la liste des TrustScores des signaleurs humains.
// Retourne exactement 0.0 si la tranche est vide.
func ComputeCrowdTrustScore(reporterTrustScores []float64) (crowdScore, maxOmega, sumOmega float64) {
	if len(reporterTrustScores) == 0 {
		return 0.0, 0.0, 0.0
	}
	for _, ts := range reporterTrustScores {
		omega := ComputeReporterExponentialImpulse(ts)
		if omega > maxOmega {
			maxOmega = omega
		}
		sumOmega += omega
	}
	crowdScore = ComputeCrowdTrustFromMaxAndSum(maxOmega, sumOmega)
	return crowdScore, maxOmega, sumOmega
}

// ComputeEconomicBonus calcule B_éco = 0.10 * ln(1 + V_éco) (§C.3.4).
func ComputeEconomicBonus(vEco float64) float64 {
	if vEco <= 0 || math.IsNaN(vEco) {
		return 0.0
	}
	return variables.ModerationEcoLogFactor * math.Log(1.0+vEco)
}

// ComputeAgingMultiplier calcule M_temps = 1.0 + (deltaHours / 12.0)^2 (§C.3.5).
func ComputeAgingMultiplier(deltaHours float64) float64 {
	if deltaHours <= 0 || math.IsNaN(deltaHours) {
		return 1.0
	}
	halfLife := variables.ModerationAgingHalfLifeHours
	if halfLife <= 0 {
		halfLife = 12.0
	}
	ratio := deltaHours / halfLife
	return 1.0 + ratio*ratio
}

// ComputeYouthBonus calcule B_jeunesse = K_q * exp(-lambda_j * accountAgeDays) (§C.6.2).
// Ne s'applique qu'aux contenus visuels ou publications (hasImageOrPost == true).
func ComputeYouthBonus(accountAgeDays float64, hasImageOrPost bool) float64 {
	if !hasImageOrPost {
		return 0.0
	}
	if accountAgeDays < 0 || math.IsNaN(accountAgeDays) {
		accountAgeDays = 0
	}
	return variables.ModerationYouthBonusMax * math.Exp(-variables.ModerationYouthDecayLambda*accountAgeDays)
}

// ResolveTargetWeight retourne le multiplicateur de cible W_cible (§C.4.1).
func ResolveTargetWeight(targetType int) float64 {
	switch targetType {
	case variables.ReportTargetPost:
		return variables.ModerationWeightTargetPost
	case variables.ReportTargetComment:
		return variables.ModerationWeightTargetComment
	case variables.ReportTargetUser:
		return variables.ModerationWeightTargetProfile
	case variables.ReportTargetMessage:
		return variables.ModerationWeightTargetMessage
	case variables.ReportTargetConversation:
		return variables.ModerationWeightTargetConv
	default:
		return variables.ModerationWeightTargetPost
	}
}

// ResolveOriginWeight retourne le multiplicateur d'origine W_origine (§C.4.2).
func ResolveOriginWeight(origin int) float64 {
	switch origin {
	case variables.ReportOriginUser:
		return variables.ModerationWeightOriginUser
	case variables.ReportOriginAutoAI:
		return variables.ModerationWeightOriginAutoAI
	case variables.ReportOriginHybrid:
		return variables.ModerationWeightOriginHybrid
	case variables.ReportOriginSystem:
		return variables.ModerationWeightOriginSystem
	default:
		return variables.ModerationWeightOriginUser
	}
}

// ResolveCategoryGravity retourne le facteur de gravité G_cat (§C.4.3).
// Les catégories critiques 6 (Mineur) et 9 (Automutilation / Suicide) retournent +Inf.
func ResolveCategoryGravity(category int) float64 {
	switch category {
	case variables.ReportCatUnderage, variables.ReportCatSelfHarm:
		return math.Inf(1)
	case variables.ReportCatSpam:
		return variables.ReportGravitySpam
	case variables.ReportCatHarassmentMoral:
		return variables.ReportGravityHarassmentMoral
	case variables.ReportCatHarassmentSexual:
		return variables.ReportGravityHarassmentSexual
	case variables.ReportCatHateSpeech:
		return variables.ReportGravityHateSpeech
	case variables.ReportCatIdentityTheft:
		return variables.ReportGravityIdentityTheft
	case variables.ReportCatNonConsensual:
		return variables.ReportGravityNonConsensual
	case variables.ReportCatIllegalContent:
		return variables.ReportGravityIllegalContent
	case variables.ReportCatAIPornography:
		return variables.ReportGravityAIPornography
	case variables.ReportCatOther:
		return variables.ReportGravityOther
	default:
		return variables.ReportGravityOther
	}
}

// IsInfiniteGravityCategory indique si la catégorie impose un traitement d'urgence vitale absolue (+Inf).
func IsInfiniteGravityCategory(category int) bool {
	return category == variables.ReportCatUnderage || category == variables.ReportCatSelfHarm
}

// ComputeMasterModerationPriority évalue l'Équation Maîtresse Unifiée (§C.2) sans aucun appel I/O.
func ComputeMasterModerationPriority(in MasterScoringInput) MasterScoringResult {
	var res MasterScoringResult

	// 1. Score IA (P_IA) et Bonus de Jeunesse (B_jeunesse)
	isVisualOrPost := in.HasImageOrPost && !in.IsPureText
	if isVisualOrPost {
		if len(in.IAScores) > 0 {
			res.IAScore = AggregateMaxIAScore(in.IAScores...)
		} else {
			res.IAScore = clampUnit(in.MaxIAScore)
		}
		res.YouthBonus = ComputeYouthBonus(in.AccountAgeDays, true)
	} else {
		// Scénario C : Texte pur, commentaire, profil, salon -> P_IA = 0 et B_jeunesse = 0
		res.IAScore = 0.0
		res.YouthBonus = 0.0
	}

	// 2. Confiance cumulée des signaleurs (S_sig)
	if len(in.ReporterTrusts) > 0 {
		res.CrowdScore, _, _ = ComputeCrowdTrustScore(in.ReporterTrusts)
	} else {
		res.CrowdScore = ComputeCrowdTrustFromMaxAndSum(in.MaxReporterOmega, in.SumReporterOmega)
	}

	// 3. Bouclier / amplificateur de l'auteur (Phi_auteur) et Bonus économique (B_éco)
	res.AuthorShield = ComputeAuthorTrustShield(in.AuthorTrustScore)
	res.EconomicBonus = ComputeEconomicBonus(in.EconomicValue)

	// 4. Score de base intrinsèque : P_base = Phi_auteur * (W_IA * P_IA + S_sig + B_éco + B_jeunesse)
	res.BasePriority = res.AuthorShield * (variables.ModerationWeightIA*res.IAScore + res.CrowdScore + res.EconomicBonus + res.YouthBonus)

	// 5. Multiplicateurs contextuels
	res.TargetWeight = ResolveTargetWeight(in.TargetType)
	res.OriginWeight = ResolveOriginWeight(in.Origin)
	res.CategoryGravity = ResolveCategoryGravity(in.Category)
	res.AgingMultiplier = ComputeAgingMultiplier(in.AgeHours)

	// 6. Court-circuit +Inf pour les catégories vitales (6 et 9)
	if math.IsInf(res.CategoryGravity, 1) {
		res.IsInfiniteUrgent = true
		res.StaticBaseScore = math.Inf(1)
		res.FinalQueueScore = math.Inf(1)
		return res
	}

	res.StaticBaseScore = res.BasePriority * res.TargetWeight * res.OriginWeight * res.CategoryGravity
	res.FinalQueueScore = res.StaticBaseScore * res.AgingMultiplier
	return res
}

// RecomputeAgedQueueScore recalcule P_file à partir du score statique hors-temps et de l'âge en heures.
// Préserve +Inf sans produire de NaN.
func RecomputeAgedQueueScore(staticBaseScore, ageHours float64) float64 {
	if math.IsInf(staticBaseScore, 1) {
		return math.Inf(1)
	}
	if staticBaseScore < 0 || math.IsNaN(staticBaseScore) {
		staticBaseScore = 0
	}
	return staticBaseScore * ComputeAgingMultiplier(ageHours)
}

// ============================================================================
// MOTEUR ELO ASYMÉTRIQUE & COLD START DU TRUST SCORE (§C.13)
// ============================================================================

// ComputeBonusInertiaFactor calcule le facteur d'amortissement des bonus :
// K_bonus(u) = max(0.10, (1.0 - T_actuel(u))^2).
func ComputeBonusInertiaFactor(currentTrust float64) float64 {
	t := clampUnit(currentTrust)
	diff := 1.0 - t
	k := diff * diff
	if k < variables.TrustELOMinKBonus {
		return variables.TrustELOMinKBonus
	}
	return k
}

// ComputeMalusSeverityFactor calcule le multiplicateur de sévérité des malus :
// K_malus(u) = max(0.50, 1.50 - T_actuel(u)).
func ComputeMalusSeverityFactor(currentTrust float64) float64 {
	t := clampUnit(currentTrust)
	k := variables.TrustELOMalusBase - t
	if k < variables.TrustELOMinKMalus {
		return variables.TrustELOMinKMalus
	}
	return k
}

// TrustTriggerSpec décrit les propriétés d'un déclencheur du dictionnaire ELO (§C.13).
type TrustTriggerSpec struct {
	CanonicalName    string
	BaseDelta        float64 // Valeur absolue |Delta_base|
	IsPositive       bool
	IsMajorBan       bool
	IsMinorStrike    bool
	IsOnboardingStep bool
}

// ResolveTrustTrigger normalise le nom du déclencheur (compatible avec les constantes historiques et §C.13).
func ResolveTrustTrigger(trigger string) (TrustTriggerSpec, bool) {
	norm := strings.ToLower(strings.TrimSpace(trigger))
	switch norm {
	case "quarantine_approved":
		return TrustTriggerSpec{
			CanonicalName:    variables.TrustEventQuarantineApproved,
			BaseDelta:        variables.TrustBonusQuarantineApproved,
			IsPositive:       true,
			IsOnboardingStep: true,
		}, true
	case variables.TrustTriggerVerifiedIdentity:
		return TrustTriggerSpec{
			CanonicalName: variables.TrustTriggerVerifiedIdentity,
			BaseDelta:     variables.TrustBonusVerifiedIdentity,
			IsPositive:    true,
		}, true
	case variables.TrustTriggerSuccessfulAppeal:
		return TrustTriggerSpec{
			CanonicalName: variables.TrustTriggerSuccessfulAppeal,
			BaseDelta:     variables.TrustBonusSuccessfulAppeal,
			IsPositive:    true,
		}, true
	case variables.TrustTriggerMonthlyClean:
		return TrustTriggerSpec{
			CanonicalName: variables.TrustTriggerMonthlyClean,
			BaseDelta:     variables.TrustBonusMonthlyClean,
			IsPositive:    true,
		}, true
	case variables.TrustTriggerLikedByAdmin:
		return TrustTriggerSpec{
			CanonicalName: variables.TrustTriggerLikedByAdmin,
			BaseDelta:     variables.TrustBonusLikedByAdmin,
			IsPositive:    true,
		}, true
	case variables.TrustTriggerJustifiedReport:
		return TrustTriggerSpec{
			CanonicalName: variables.TrustTriggerJustifiedReport,
			BaseDelta:     variables.TrustBonusJustifiedReport,
			IsPositive:    true,
		}, true
	case variables.TrustTriggerModBatchLike:
		return TrustTriggerSpec{
			CanonicalName: variables.TrustTriggerModBatchLike,
			BaseDelta:     variables.TrustBonusModBatchLike,
			IsPositive:    true,
		}, true
	case variables.TrustTriggerSpamDetectedAuto:
		return TrustTriggerSpec{
			CanonicalName: variables.TrustTriggerSpamDetectedAuto,
			BaseDelta:     variables.TrustMalusSpamDetectedAuto,
			IsPositive:    false,
		}, true
	case variables.TrustTriggerMutedInPublicComm:
		return TrustTriggerSpec{
			CanonicalName: variables.TrustTriggerMutedInPublicComm,
			BaseDelta:     variables.TrustMalusMutedInPublicComm,
			IsPositive:    false,
		}, true
	case variables.TrustTriggerAbusiveReport:
		return TrustTriggerSpec{
			CanonicalName: variables.TrustTriggerAbusiveReport,
			BaseDelta:     variables.TrustMalusAbusiveReport,
			IsPositive:    false,
		}, true
	case variables.TrustTriggerMinorInfraction:
		return TrustTriggerSpec{
			CanonicalName: variables.TrustTriggerMinorInfraction,
			BaseDelta:     variables.TrustMalusMinorInfraction,
			IsPositive:    false,
			IsMinorStrike: true,
		}, true
	case variables.TrustTriggerMajorInfraction:
		return TrustTriggerSpec{
			CanonicalName: variables.TrustTriggerMajorInfraction,
			BaseDelta:     variables.TrustMalusMajorInfraction,
			IsPositive:    false,
			IsMajorBan:    true,
		}, true
	default:
		return TrustTriggerSpec{}, false
	}
}

// ComputeAsymmetricELO applique la dynamique ELO asymétrique ou le bonus linéaire de Cold Start (§C.13).
// Retourne :
// - newTrust : nouveau TrustScore borné dans [0.00, 1.00]
// - appliedDelta : variation effective signée (newTrust - currentTrust)
// - newOnboardingApproved : compteur mis à jour des publications de quarantaine approuvées
// - spec : métadonnées du déclencheur
func ComputeAsymmetricELO(currentTrust float64, trigger string, onboardingApprovedCount int) (newTrust, appliedDelta float64, newOnboardingApproved int, spec TrustTriggerSpec, ok bool) {
	currentTrust = clampUnit(currentTrust)
	newOnboardingApproved = onboardingApprovedCount

	spec, ok = ResolveTrustTrigger(trigger)
	if !ok {
		return currentTrust, 0.0, newOnboardingApproved, spec, false
	}

	if spec.IsMajorBan {
		return variables.TrustScoreHardBan, -currentTrust, newOnboardingApproved, spec, true
	}

	// Cold Start déterministe (§C.13) : les N_posts (4) premières validations en quarantaine
	// accordent un bonus linéaire net de +0.05 chacune pour atteindre exactement 0.40.
	if spec.IsOnboardingStep && onboardingApprovedCount < variables.TrustOnboardingPostsRequired && currentTrust < variables.TrustThresholdStandard {
		newOnboardingApproved = onboardingApprovedCount + 1
		rawDelta := variables.TrustOnboardingStepBonus
		newTrust = clampUnit(math.Round((currentTrust+rawDelta)*10000) / 10000)
		return newTrust, newTrust - currentTrust, newOnboardingApproved, spec, true
	}

	if spec.IsPositive {
		if spec.IsOnboardingStep {
			newOnboardingApproved = onboardingApprovedCount + 1
		}
		kBonus := ComputeBonusInertiaFactor(currentTrust)
		rawDelta := spec.BaseDelta * kBonus
		newTrust = clampUnit(currentTrust + rawDelta)
		return newTrust, newTrust - currentTrust, newOnboardingApproved, spec, true
	}

	kMalus := ComputeMalusSeverityFactor(currentTrust)
	rawDelta := -spec.BaseDelta * kMalus
	newTrust = clampUnit(currentTrust + rawDelta)
	return newTrust, newTrust - currentTrust, newOnboardingApproved, spec, true
}

// ResolveInitialTrustScoreForGrade détermine le TrustScore initial selon le grade du compte (§C.13).
// Les Administrateurs (4), Modérateurs (3) et Partenaires (2) bénéficient de T_init,VIP = 0.85.
func ResolveInitialTrustScoreForGrade(grade int) float64 {
	if grade == variables.UserGradeAdmin || grade == variables.UserGradeModerator || grade == variables.UserGradeParter {
		return variables.TrustScoreInitialVIP
	}
	return variables.TrustScoreInitial
}

func clampUnit(v float64) float64 {
	if math.IsNaN(v) || v < variables.TrustScoreMin {
		return variables.TrustScoreMin
	}
	if v > variables.TrustScoreMax {
		return variables.TrustScoreMax
	}
	return v
}
