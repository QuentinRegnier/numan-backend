package community_recommendation_service

import (
	"math"
	"sort"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// LoadScoringConfig lit les variables dynamiques courantes dans un instantané immuable.
func LoadScoringConfig() community_recommendation_models.ScoringConfig {
	return community_recommendation_models.ScoringConfig{
		WeightMembers:        variables.CommunityRecoWeightMembers,
		WeightDynamism:       variables.CommunityRecoWeightDynamism,
		WeightGrowth:         variables.CommunityRecoWeightGrowth,
		AlphaOwner:           variables.CommunityRecoTrustAlphaOwner,
		AlphaAdmin:           variables.CommunityRecoTrustAlphaAdmin,
		AlphaMember:          variables.CommunityRecoTrustAlphaMember,
		TrustPriorMean:       variables.CommunityRecoTrustPriorMean,
		TrustPriorStrength:   variables.CommunityRecoTrustPriorStrength,
		TrustMemberSampleCap: variables.CommunityRecoTrustMemberSampleCap,
		ModeMultFreeChat:     variables.CommunityRecoModeMultFreeChat,
		ModeMultSubreddit:    variables.CommunityRecoModeMultSubreddit,
		ModeMultAnnouncement: variables.CommunityRecoModeMultAnnouncement,
		PartnerMultiplier:    variables.CommunityRecoPartnerMultiplier,
		FreshnessBoost:       variables.CommunityRecoFreshnessBoost,
		FreshnessDays:        variables.CommunityRecoFreshnessDays,
		FriendGamma:          variables.CommunityRecoFriendBoostGamma,
	}
}

func sanitizeNonNegative(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return v
}

func clampUnit(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0.5
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ComputeHierarchicalTrust calcule le score de confiance agrégé T_comm ∈ [0, 1]
// et le multiplicateur M_trust = 0.5 + T_comm ∈ [0.5, 1.5].
//
// Règles métier :
//   - Owner absent (hasOwnerTrust=false) -> repli sur priorMean (0.50).
//   - Aucun admin délégué -> redistribution exacte des poids entre Owner et Membres :
//     wOwner' = alphaOwner / (alphaOwner + alphaMember), wMember' = alphaMember / (alphaOwner + alphaMember).
//   - Lissage bayésien des membres : (C * mu0 + sum(T_i)) / (C + n), échantillon borné à TrustMemberSampleCap.
func ComputeHierarchicalTrust(
	hasOwnerTrust bool,
	ownerTrust float64,
	adminTrusts []float64,
	memberTrusts []float64,
	cfg community_recommendation_models.ScoringConfig,
) (float64, float64) {
	priorMean := clampUnit(cfg.TrustPriorMean)
	priorStrength := sanitizeNonNegative(cfg.TrustPriorStrength)

	tOwner := priorMean
	if hasOwnerTrust && !math.IsNaN(ownerTrust) && !math.IsInf(ownerTrust, 0) {
		tOwner = clampUnit(ownerTrust)
	}

	// Lissage bayésien de la base membres (capé à TrustMemberSampleCap)
	sample := memberTrusts
	if cfg.TrustMemberSampleCap > 0 && len(sample) > cfg.TrustMemberSampleCap {
		sample = sample[:cfg.TrustMemberSampleCap]
	}

	var memberSum float64
	validMembers := 0
	for _, ts := range sample {
		if math.IsNaN(ts) || math.IsInf(ts, 0) {
			continue
		}
		memberSum += clampUnit(ts)
		validMembers++
	}

	var tMember float64
	denomBayes := priorStrength + float64(validMembers)
	if denomBayes > 0 {
		tMember = (priorStrength*priorMean + memberSum) / denomBayes
	} else {
		tMember = priorMean
	}

	// Moyenne des admins délégués si présents
	var adminSum float64
	validAdmins := 0
	for _, ts := range adminTrusts {
		if math.IsNaN(ts) || math.IsInf(ts, 0) {
			continue
		}
		adminSum += clampUnit(ts)
		validAdmins++
	}

	alphaOwner := sanitizeNonNegative(cfg.AlphaOwner)
	alphaAdmin := sanitizeNonNegative(cfg.AlphaAdmin)
	alphaMember := sanitizeNonNegative(cfg.AlphaMember)

	var tComm float64
	if validAdmins == 0 {
		denom := alphaOwner + alphaMember
		if denom > 0 {
			wOwner := alphaOwner / denom
			wMember := alphaMember / denom
			tComm = wOwner*tOwner + wMember*tMember
		} else {
			tComm = priorMean
		}
	} else {
		tAdmin := adminSum / float64(validAdmins)
		denom := alphaOwner + alphaAdmin + alphaMember
		if denom > 0 {
			tComm = (alphaOwner*tOwner + alphaAdmin*tAdmin + alphaMember*tMember) / denom
		} else {
			tComm = priorMean
		}
	}

	tComm = clampUnit(tComm)
	mTrust := 0.5 + tComm
	return tComm, mTrust
}

// ResolveModeMultiplier retourne le multiplicateur K_mode associé au mode d'interaction.
func ResolveModeMultiplier(mode models.CommunityMode, cfg community_recommendation_models.ScoringConfig) float64 {
	var mult float64
	switch mode {
	case models.CommunityModeSubreddit:
		mult = cfg.ModeMultSubreddit
	case models.CommunityModeAnnouncement:
		mult = cfg.ModeMultAnnouncement
	case models.CommunityModeFreeChat:
		mult = cfg.ModeMultFreeChat
	default:
		mult = cfg.ModeMultFreeChat
	}
	mult = sanitizeNonNegative(mult)
	if mult == 0 {
		return 1.0
	}
	return mult
}

// ComputeFreshnessMultiplier calcule la décroissance linéaire de fraîcheur en UTC :
// 1.0 + boost à t=0 jusqu'à 1.00 au bout de freshnessDays jours.
func ComputeFreshnessMultiplier(createdAt time.Time, now time.Time, boost float64, freshnessDays int) float64 {
	boost = sanitizeNonNegative(boost)
	if boost == 0 || freshnessDays <= 0 || createdAt.IsZero() {
		return 1.0
	}
	if now.IsZero() {
		now = createdAt
	}

	ageDays := now.UTC().Sub(createdAt.UTC()).Hours() / 24.0
	if ageDays <= 0 {
		return 1.0 + boost
	}
	window := float64(freshnessDays)
	if ageDays >= window {
		return 1.0
	}
	return 1.0 + boost*(1.0-(ageDays/window))
}

// ComputeGlobalScoreWithBreakdown calcule le score global S_global et renvoie le détail complet.
//
// Formule :
//
//	S_members  = ln(1 + member_count)
//	S_dynamism = sqrt(ln(1 + msg_7d) * ln(1 + unique_authors_7d))
//	S_growth   = ln(1 + growth_7d)
//	S_base     = w_M * S_members + w_D * S_dynamism + w_G * S_growth
//	S_global   = S_base * M_trust * K_mode * K_partner * K_fresh
func ComputeGlobalScoreWithBreakdown(
	in community_recommendation_models.CommunityScoreInput,
	cfg community_recommendation_models.ScoringConfig,
) community_recommendation_models.GlobalScoreBreakdown {
	members := in.MemberCount
	if members < 0 {
		members = 0
	}
	msgs := in.MsgCount7d
	if msgs < 0 {
		msgs = 0
	}
	authors := in.UniqueAuthors7d
	if authors < 0 {
		authors = 0
	}
	growth := in.Growth7d
	if growth < 0 {
		growth = 0
	}

	sMembers := math.Log1p(float64(members))
	sDynamism := math.Sqrt(math.Log1p(float64(msgs)) * math.Log1p(float64(authors)))
	sGrowth := math.Log1p(float64(growth))

	wM := sanitizeNonNegative(cfg.WeightMembers)
	wD := sanitizeNonNegative(cfg.WeightDynamism)
	wG := sanitizeNonNegative(cfg.WeightGrowth)
	sBase := wM*sMembers + wD*sDynamism + wG*sGrowth

	tComm, mTrust := ComputeHierarchicalTrust(in.HasOwnerTrust, in.OwnerTrust, in.AdminTrusts, in.MemberTrusts, cfg)
	kMode := ResolveModeMultiplier(in.Mode, cfg)

	kPartner := 1.0
	if in.IsPartner {
		p := sanitizeNonNegative(cfg.PartnerMultiplier)
		if p > 0 {
			kPartner = p
		}
	}

	kFresh := ComputeFreshnessMultiplier(in.CreatedAt, in.Now, cfg.FreshnessBoost, cfg.FreshnessDays)
	sGlobal := sanitizeNonNegative(sBase * mTrust * kMode * kPartner * kFresh)

	return community_recommendation_models.GlobalScoreBreakdown{
		MembersScore:        sMembers,
		DynamismScore:       sDynamism,
		GrowthScore:         sGrowth,
		BaseScore:           sBase,
		CommunityTrust:      tComm,
		TrustMultiplier:     mTrust,
		ModeMultiplier:      kMode,
		PartnerMultiplier:   kPartner,
		FreshnessMultiplier: kFresh,
		GlobalScore:         sGlobal,
	}
}

// ComputeGlobalScore retourne directement le score global S_global d'une communauté.
func ComputeGlobalScore(
	in community_recommendation_models.CommunityScoreInput,
	cfg community_recommendation_models.ScoringConfig,
) float64 {
	return ComputeGlobalScoreWithBreakdown(in, cfg).GlobalScore
}

// ComputeInitialCreationScore calcule le score initial immédiat d'une nouvelle communauté
// publique lors de sa création (avant le prochain cycle Cron).
func ComputeInitialCreationScore(
	mode models.CommunityMode,
	isPartner bool,
	hasOwnerTrust bool,
	ownerTrust float64,
	initialMembers int,
	now time.Time,
	cfg community_recommendation_models.ScoringConfig,
) float64 {
	if initialMembers < 1 {
		initialMembers = 1
	}
	return ComputeGlobalScore(community_recommendation_models.CommunityScoreInput{
		MemberCount:   initialMembers,
		HasOwnerTrust: hasOwnerTrust,
		OwnerTrust:    ownerTrust,
		Mode:          mode,
		IsPartner:     isPartner,
		CreatedAt:     now,
		Now:           now,
	}, cfg)
}

// ApplyFriendBoost applique le boost logarithmique d'affinité amis de degré 1 :
//
//	S_perso = S_global * (1 + gamma * ln(1 + n_friends))
//
// et trie les candidats par S_perso décroissant avec bris d'égalité déterministe
// sur conversation_id croissant.
func ApplyFriendBoost(
	candidates []community_recommendation_models.RankedCandidate,
	friendCounts map[int64]int,
	gamma float64,
) []community_recommendation_models.PersonalizedCandidate {
	if len(candidates) == 0 {
		return []community_recommendation_models.PersonalizedCandidate{}
	}
	gamma = sanitizeNonNegative(gamma)

	out := make([]community_recommendation_models.PersonalizedCandidate, len(candidates))
	for i, c := range candidates {
		fc := 0
		if friendCounts != nil && friendCounts[c.ConversationID] > 0 {
			fc = friendCounts[c.ConversationID]
		}
		gScore := sanitizeNonNegative(c.GlobalScore)
		pScore := gScore
		if fc > 0 && gamma > 0 {
			pScore = gScore * (1.0 + gamma*math.Log1p(float64(fc)))
		}
		out[i] = community_recommendation_models.PersonalizedCandidate{
			ConversationID:    c.ConversationID,
			GlobalScore:       gScore,
			PersonalizedScore: sanitizeNonNegative(pScore),
			FriendCount:       fc,
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].PersonalizedScore != out[j].PersonalizedScore {
			return out[i].PersonalizedScore > out[j].PersonalizedScore
		}
		return out[i].ConversationID < out[j].ConversationID
	})

	return out
}

// InjectPinnedSlots assemble la liste complète de recommandation en plaçant chaque
// communauté épinglée à sa position exacte 1-based (`slot`), tout en la retirant
// du flux organique (zéro doublon, zéro trou vide, compaction des slots épars).
//
// `extraPinned` permet optionnellement de fournir des communautés épinglées actives
// absentes temporairement du ZSET organique. Toute entrée dans `pins` absente à la
// fois de `organic` et de `extraPinned` est ignorée (pin périmé/inactif).
func InjectPinnedSlots(
	organic []community_recommendation_models.PersonalizedCandidate,
	pins map[int]int64,
	extraPinned map[int64]community_recommendation_models.PersonalizedCandidate,
) []community_recommendation_models.PersonalizedCandidate {
	if len(pins) == 0 {
		result := make([]community_recommendation_models.PersonalizedCandidate, len(organic))
		copy(result, organic)
		return result
	}

	organicByID := make(map[int64]community_recommendation_models.PersonalizedCandidate, len(organic))
	for _, c := range organic {
		organicByID[c.ConversationID] = c
	}

	// 1. Trier les slots par ordre croissant pour un traitement déterministe
	slots := make([]int, 0, len(pins))
	for slot, convID := range pins {
		if slot > 0 && convID > 0 {
			slots = append(slots, slot)
		}
	}
	sort.Ints(slots)

	type resolvedPin struct {
		slot      int
		candidate community_recommendation_models.PersonalizedCandidate
	}

	pinnedIDs := make(map[int64]struct{}, len(slots))
	validPins := make([]resolvedPin, 0, len(slots))

	for _, slot := range slots {
		convID := pins[slot]
		// Déduplication défensive si le même convID apparaissait sur 2 slots
		if _, alreadyPinned := pinnedIDs[convID]; alreadyPinned {
			continue
		}

		cand, ok := organicByID[convID]
		if !ok && extraPinned != nil {
			cand, ok = extraPinned[convID]
		}
		if !ok {
			continue
		}

		cand.IsPinned = true
		cand.PinSlot = slot

		pinnedIDs[convID] = struct{}{}
		validPins = append(validPins, resolvedPin{
			slot:      slot,
			candidate: cand,
		})
	}

	if len(validPins) == 0 {
		result := make([]community_recommendation_models.PersonalizedCandidate, len(organic))
		copy(result, organic)
		return result
	}

	// 2. Retirer les communautés épinglées du flux organique (zéro doublon)
	remainingOrganic := make([]community_recommendation_models.PersonalizedCandidate, 0, len(organic))
	for _, c := range organic {
		if _, isPinned := pinnedIDs[c.ConversationID]; isPinned {
			continue
		}
		remainingOrganic = append(remainingOrganic, c)
	}

	// 3. Fusion à deux pointeurs : slots exacts 1-based, zéro trou vide, compaction des slots épars
	total := len(validPins) + len(remainingOrganic)
	assembled := make([]community_recommendation_models.PersonalizedCandidate, 0, total)
	pinIdx := 0
	orgIdx := 0

	for len(assembled) < total {
		currentSlot := len(assembled) + 1 // 1-based
		if pinIdx < len(validPins) && (validPins[pinIdx].slot <= currentSlot || orgIdx >= len(remainingOrganic)) {
			assembled = append(assembled, validPins[pinIdx].candidate)
			pinIdx++
		} else {
			assembled = append(assembled, remainingOrganic[orgIdx])
			orgIdx++
		}
	}

	return assembled
}

// PaginateCandidates découpe la liste assemblée complète selon (offset, limit)
// et renvoie la page ainsi que l'indicateur hasMore.
func PaginateCandidates(
	assembled []community_recommendation_models.PersonalizedCandidate,
	offset int,
	limit int,
) ([]community_recommendation_models.PersonalizedCandidate, bool) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		return []community_recommendation_models.PersonalizedCandidate{}, offset < len(assembled)
	}
	if offset >= len(assembled) {
		return []community_recommendation_models.PersonalizedCandidate{}, false
	}
	end := offset + limit
	if end > len(assembled) {
		end = len(assembled)
	}
	hasMore := end < len(assembled)
	page := make([]community_recommendation_models.PersonalizedCandidate, end-offset)
	copy(page, assembled[offset:end])
	return page, hasMore
}
