package moderation_service

import (
	"math"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

func TestScoringEngine_ReporterWeightAndCrowdDamping(t *testing.T) {
	// 1. Aucun signaleur humain -> S_sig = 0.0 exactement (pas de NaN)
	crowdEmpty, maxEmpty, sumEmpty := ComputeCrowdTrustScore(nil)
	if crowdEmpty != 0.0 || maxEmpty != 0.0 || sumEmpty != 0.0 || math.IsNaN(crowdEmpty) {
		t.Fatalf("attendu S_sig=0.0 pour tranche vide, obtenu crowd=%v max=%v sum=%v", crowdEmpty, maxEmpty, sumEmpty)
	}
	if got := ComputeCrowdTrustFromMaxAndSum(0, 0); got != 0.0 || math.IsNaN(got) {
		t.Fatalf("attendu 0.0 pour max=0,sum=0, obtenu %v", got)
	}

	// 2. Valeurs exactes de omega_j : T=0.20 -> ~0.0229, T=0.50 -> ~0.1192, T=1.00 -> 1.0000
	w020 := ComputeReporterExponentialImpulse(0.20)
	w050 := ComputeReporterExponentialImpulse(0.50)
	w100 := ComputeReporterExponentialImpulse(1.00)
	if math.Abs(w020-0.0229) > 0.001 {
		t.Errorf("omega(0.20) attendu ~0.0229, obtenu %.4f", w020)
	}
	if math.Abs(w050-0.1192) > 0.001 {
		t.Errorf("omega(0.50) attendu ~0.1192, obtenu %.4f", w050)
	}
	if math.Abs(w100-1.0000) > 1e-6 {
		t.Errorf("omega(1.00) attendu 1.0000, obtenu %.6f", w100)
	}

	// 3. Anti-brigading : 50 comptes neufs (T=0.20) vs 1 VIP (T=1.00)
	brigade := make([]float64, 50)
	for i := range brigade {
		brigade[i] = 0.20
	}
	sBrigade, _, _ := ComputeCrowdTrustScore(brigade)
	sSingleVIP, _, _ := ComputeCrowdTrustScore([]float64{1.00})
	// S_sig(50 x 0.20) = 0.0229 + 0.50 * ln(1 + 50*0.0229) ≈ 0.404 < S_sig(1 x 1.00) = 1.0 + 0.50*ln(2) ≈ 1.346
	if sBrigade >= sSingleVIP {
		t.Errorf("anti-brigading en échec : 50 comptes T=0.20 (%f) >= 1 compte VIP T=1.00 (%f)", sBrigade, sSingleVIP)
	}
}

func TestScoringEngine_AuthorShieldAndEcoAndAgingAndYouth(t *testing.T) {
	// 1. Bouclier / amplificateur de l'auteur Phi_auteur (§C.3.3)
	phiZero := ComputeAuthorTrustShield(0.00) // 1.50
	phiNew := ComputeAuthorTrustShield(0.20)  // 1.40 / 1.06 ≈ 1.32075
	phiVIP := ComputeAuthorTrustShield(1.00)  // 1.00 / 2.50 = 0.40
	if math.Abs(phiZero-1.50) > 1e-6 {
		t.Errorf("Phi(0.00) attendu 1.50, obtenu %f", phiZero)
	}
	if math.Abs(phiNew-(1.40/1.06)) > 1e-5 {
		t.Errorf("Phi(0.20) attendu ~1.32075, obtenu %f", phiNew)
	}
	if math.Abs(phiVIP-0.40) > 1e-6 {
		t.Errorf("Phi(1.00) attendu 0.40, obtenu %f", phiVIP)
	}

	// 2. Bonus économique B_éco = 0.10 * ln(1 + V_éco) (§C.3.4)
	if got := ComputeEconomicBonus(0); got != 0.0 {
		t.Errorf("B_éco(0) attendu 0.0, obtenu %f", got)
	}
	expectedEco := 0.10 * math.Log(1.0+1000.0)
	if got := ComputeEconomicBonus(1000.0); math.Abs(got-expectedEco) > 1e-6 {
		t.Errorf("B_éco(1000) attendu %f, obtenu %f", expectedEco, got)
	}

	// 3. Multiplicateur de vieillissement M_temps = 1.0 + (dt / 12.0)^2 (§C.3.5)
	if got := ComputeAgingMultiplier(0); math.Abs(got-1.0) > 1e-6 {
		t.Errorf("M_temps(0h) attendu 1.0, obtenu %f", got)
	}
	if got := ComputeAgingMultiplier(12); math.Abs(got-2.0) > 1e-6 {
		t.Errorf("M_temps(12h) attendu 2.0, obtenu %f", got)
	}
	if got := ComputeAgingMultiplier(24); math.Abs(got-5.0) > 1e-6 {
		t.Errorf("M_temps(24h) attendu 5.0, obtenu %f", got)
	}

	// 4. Bonus de jeunesse B_jeunesse = 5.0 * exp(-0.05 * jours) (§C.6.2)
	b1d := ComputeYouthBonus(1.0, true)     // ~4.756
	b730d := ComputeYouthBonus(730.0, true) // ~0.000
	bText := ComputeYouthBonus(1.0, false)  // 0.0 pour texte pur
	if math.Abs(b1d-4.756147) > 1e-3 {
		t.Errorf("B_jeunesse(1j) attendu ~4.756, obtenu %f", b1d)
	}
	if b730d > 1e-10 {
		t.Errorf("B_jeunesse(730j) attendu ~0.000, obtenu %e", b730d)
	}
	if bText != 0.0 {
		t.Errorf("B_jeunesse pour texte pur attendu 0.0, obtenu %f", bText)
	}
}

func TestScoringEngine_MasterEquationAndCrossScenarioInvariants(t *testing.T) {
	// 1. Catégories 6 (Mineur) et 9 (Automutilation/Suicide) -> +Inf immédiat
	for _, cat := range []int{variables.ReportCatUnderage, variables.ReportCatSelfHarm} {
		if !IsInfiniteGravityCategory(cat) {
			t.Errorf("catégorie vitale %d non reconnue par IsInfiniteGravityCategory", cat)
		}
		res := ComputeMasterModerationPriority(MasterScoringInput{
			TargetType:       variables.ReportTargetPost,
			Origin:           variables.ReportOriginUser,
			Category:         cat,
			AuthorTrustScore: 0.95,
			ReporterTrusts:   []float64{0.20},
		})
		if !res.IsInfiniteUrgent || !math.IsInf(res.FinalQueueScore, 1) {
			t.Fatalf("catégorie vitale %d devrait produire +Inf, obtenu %v", cat, res.FinalQueueScore)
		}
		if aged := RecomputeAgedQueueScore(res.StaticBaseScore, 36.0); !math.IsInf(aged, 1) {
			t.Fatalf("RecomputeAgedQueueScore sur +Inf doit conserver +Inf, obtenu %v", aged)
		}
	}

	// 2. Scénario C (Texte pur) : P_IA et B_jeunesse forcés à 0, et Hybride (2.0) vaut exactement 2x User (1.0)
	textUser := ComputeMasterModerationPriority(MasterScoringInput{
		TargetType:       variables.ReportTargetComment,
		Origin:           variables.ReportOriginUser,
		Category:         variables.ReportCatHarassmentMoral,
		IAScores:         []float64{0.99}, // Doit être ignoré car IsPureText=true
		HasImageOrPost:   false,
		IsPureText:       true,
		AuthorTrustScore: 0.30,
		AccountAgeDays:   1.0, // Doit être ignoré car IsPureText=true
		ReporterTrusts:   []float64{0.75},
		EconomicValue:    50.0,
	})
	if textUser.IAScore != 0.0 || textUser.YouthBonus != 0.0 {
		t.Fatalf("texte pur doit avoir P_IA=0 et B_jeunesse=0, obtenu P_IA=%f B_jeunesse=%f", textUser.IAScore, textUser.YouthBonus)
	}

	textHybrid := ComputeMasterModerationPriority(MasterScoringInput{
		TargetType:       variables.ReportTargetComment,
		Origin:           variables.ReportOriginHybrid,
		Category:         variables.ReportCatHarassmentMoral,
		IsPureText:       true,
		AuthorTrustScore: 0.30,
		ReporterTrusts:   []float64{0.75},
		EconomicValue:    50.0,
	})
	if math.Abs(textHybrid.FinalQueueScore-2.0*textUser.FinalQueueScore) > 1e-9 {
		t.Fatalf("Hybride (W=2.0) doit valoir exactement 2x User (W=1.0) à entrées égales : hybrid=%f vs 2*user=%f",
			textHybrid.FinalQueueScore, 2.0*textUser.FinalQueueScore)
	}

	// 3. Message privé (W_cible = 3.0) surclasse strictement un Post public (W_cible = 1.0) à gravité égale
	postTicket := ComputeMasterModerationPriority(MasterScoringInput{
		TargetType:       variables.ReportTargetPost,
		Origin:           variables.ReportOriginAutoAI,
		Category:         variables.ReportCatHarassmentSexual,
		MaxIAScore:       0.75,
		HasImageOrPost:   true,
		AuthorTrustScore: 0.40,
		AccountAgeDays:   30,
	})
	msgTicket := ComputeMasterModerationPriority(MasterScoringInput{
		TargetType:       variables.ReportTargetMessage,
		Origin:           variables.ReportOriginAutoAI,
		Category:         variables.ReportCatHarassmentSexual,
		MaxIAScore:       0.75,
		HasImageOrPost:   true,
		AuthorTrustScore: 0.40,
		AccountAgeDays:   30,
	})
	if math.Abs(msgTicket.FinalQueueScore-3.0*postTicket.FinalQueueScore) > 1e-9 {
		t.Fatalf("Message privé (W=3.0) doit valoir 3x Post (W=1.0) : msg=%f vs post=%f",
			msgTicket.FinalQueueScore, postTicket.FinalQueueScore)
	}

	// 4. Équité inter-scénarios : un ticket texte pur d'un signaleur à faible TrustScore vieux de 24h (M_temps = 5.0)
	// finit par dépasser un ticket image frais (Age=0h) modéré.
	freshImage := ComputeMasterModerationPriority(MasterScoringInput{
		TargetType:       variables.ReportTargetPost,
		Origin:           variables.ReportOriginAutoAI,
		Category:         variables.ReportCatAIPornography, // G_cat = 1.0
		MaxIAScore:       0.66,
		HasImageOrPost:   true,
		AuthorTrustScore: 0.60,
		AccountAgeDays:   120,
		AgeHours:         0,
	})
	agedTextHateSpeech := ComputeMasterModerationPriority(MasterScoringInput{
		TargetType:       variables.ReportTargetPost,
		Origin:           variables.ReportOriginUser,
		Category:         variables.ReportCatHateSpeech, // G_cat = 3.0
		IsPureText:       true,
		AuthorTrustScore: 0.25,
		ReporterTrusts:   []float64{0.35, 0.40},
		EconomicValue:    200,
		AgeHours:         24.0, // M_temps = 5.0
	})
	if agedTextHateSpeech.FinalQueueScore <= freshImage.FinalQueueScore {
		t.Fatalf("le ticket texte vieux de 24h (%f) devrait dépasser le ticket image frais (%f)",
			agedTextHateSpeech.FinalQueueScore, freshImage.FinalQueueScore)
	}
}

func TestScoringEngine_AsymmetricELOAndColdStart(t *testing.T) {
	// 1. Cold Start déterministe : 4 posts approuvés * +0.05 = 0.40 exactement (sortie de quarantaine)
	trust := variables.TrustScoreInitial // 0.20
	approved := 0
	for i := 1; i <= 4; i++ {
		var ok bool
		trust, _, approved, _, ok = ComputeAsymmetricELO(trust, variables.TrustEventQuarantineApproved, approved)
		if !ok {
			t.Fatalf("itération %d : déclencheur QUARANTINE_APPROVED non reconnu", i)
		}
	}
	if math.Abs(trust-0.40) > 1e-6 || approved != 4 {
		t.Fatalf("après 4 posts validés en Cold Start, attendu Trust=0.40 et approved=4, obtenu Trust=%f approved=%d", trust, approved)
	}

	// 2. Exemption VIP à l'inscription : Admin (4), Modérateur (3), Partenaire (2) -> 0.85 ; User (0) -> 0.20
	if got := ResolveInitialTrustScoreForGrade(variables.UserGradeAdmin); got != 0.85 {
		t.Errorf("Grade Admin attendu 0.85, obtenu %f", got)
	}
	if got := ResolveInitialTrustScoreForGrade(variables.UserGradeModerator); got != 0.85 {
		t.Errorf("Grade Moderator attendu 0.85, obtenu %f", got)
	}
	if got := ResolveInitialTrustScoreForGrade(variables.UserGradeParter); got != 0.85 {
		t.Errorf("Grade Partner attendu 0.85, obtenu %f", got)
	}
	if got := ResolveInitialTrustScoreForGrade(variables.UserGradeUser); got != 0.20 {
		t.Errorf("Grade User attendu 0.20, obtenu %f", got)
	}

	// 3. Inertie des bonus K_bonus : T=0.90 avec monthly_clean (+0.05) -> K_bonus = max(0.10, 0.01) = 0.10 -> +0.005
	newTBonus, deltaBonus, _, _, ok := ComputeAsymmetricELO(0.90, variables.TrustTriggerMonthlyClean, 4)
	if !ok || math.Abs(deltaBonus-0.005) > 1e-6 || math.Abs(newTBonus-0.905) > 1e-6 {
		t.Errorf("bonus ELO à T=0.90 attendu +0.005 (0.905), obtenu delta=%f newT=%f", deltaBonus, newTBonus)
	}

	// 4. Sévérité des malus K_malus (§C.13) :
	// - T=0.90 avec minor_infraction (-0.25) -> K_malus = 1.50 - 0.90 = 0.60 -> -0.15 -> T=0.75
	newTMalusVIP, deltaMalusVIP, _, _, _ := ComputeAsymmetricELO(0.90, variables.TrustTriggerMinorInfraction, 4)
	if math.Abs(deltaMalusVIP-(-0.15)) > 1e-6 || math.Abs(newTMalusVIP-0.75) > 1e-6 {
		t.Errorf("malus ELO à T=0.90 attendu -0.15 (0.75), obtenu delta=%f newT=%f", deltaMalusVIP, newTMalusVIP)
	}

	// - T=0.30 avec minor_infraction (-0.25) -> K_malus = 1.50 - 0.30 = 1.20 -> -0.30 -> borné à 0.00
	newTMalusLow, _, _, _, _ := ComputeAsymmetricELO(0.30, variables.TrustTriggerMinorInfraction, 1)
	if newTMalusLow != 0.00 {
		t.Errorf("malus ELO à T=0.30 attendu 0.00 (clamped), obtenu %f", newTMalusLow)
	}

	// 5. Vérification des 11 déclencheurs du dictionnaire (§C.13)
	allTriggers := []string{
		variables.TrustTriggerVerifiedIdentity,
		variables.TrustTriggerSuccessfulAppeal,
		variables.TrustTriggerMonthlyClean,
		variables.TrustTriggerLikedByAdmin,
		variables.TrustTriggerJustifiedReport,
		variables.TrustTriggerModBatchLike,
		variables.TrustTriggerSpamDetectedAuto,
		variables.TrustTriggerMutedInPublicComm,
		variables.TrustTriggerAbusiveReport,
		variables.TrustTriggerMinorInfraction,
		variables.TrustTriggerMajorInfraction,
	}
	for _, trig := range allTriggers {
		if _, ok := ResolveTrustTrigger(trig); !ok {
			t.Errorf("déclencheur obligatoire '%s' non résolu par ResolveTrustTrigger", trig)
		}
	}
}
