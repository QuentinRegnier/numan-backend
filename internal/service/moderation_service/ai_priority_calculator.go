package moderation_service

import (
	"context"
	"fmt"
	"math"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/report_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// TargetReportAggregation stocke en RAM L1 l'état d'agrégation mathématique des signalements sur une cible.
type TargetReportAggregation struct {
	ReportID        int64             `msgpack:"report_id" json:"report_id"`
	TargetType      int               `msgpack:"target_type" json:"target_type"`
	TargetID        int64             `msgpack:"target_id" json:"target_id"`
	TargetOwnerID   int64             `msgpack:"target_owner_id" json:"target_owner_id"`
	ReportOrigin    int               `msgpack:"report_origin" json:"report_origin"` // 0: User, 1: Auto_AI, 2: Hybrid
	IAScoreMax      float64           `msgpack:"ia_score_max" json:"ia_score_max"`
	MaxOmega        float64           `msgpack:"max_omega" json:"max_omega"`
	SumOmega        float64           `msgpack:"sum_omega" json:"sum_omega"`
	ReporterWeights map[int64]float64 `msgpack:"reporter_weights" json:"reporter_weights"`
	ReportCount     int               `msgpack:"report_count" json:"report_count"`
	CreatedAt       int64             `msgpack:"created_at" json:"created_at"`
}

// AggregateMaxIAScore calcule Score_IA_Post = MAX(P_ia_img1, ..., P_ia_img4) borné dans [0.0, 1.0].
func AggregateMaxIAScore(scores ...float64) float64 {
	maxScore := 0.0
	for _, s := range scores {
		if math.IsNaN(s) || math.IsInf(s, 0) {
			continue
		}
		clamped := math.Max(0.0, math.Min(1.0, s))
		if clamped > maxScore {
			maxScore = clamped
		}
	}
	return math.Round(maxScore*10000) / 10000
}

// ComputeAutoAIRiskIndex calcule R_auto = P_ia * (1 + alpha_def * (1 - T_auteur)).
func ComputeAutoAIRiskIndex(iaScore float64, authorTrustScore float64) float64 {
	ia := AggregateMaxIAScore(iaScore)
	trust := math.Max(0.0, math.Min(1.0, authorTrustScore))
	risk := ia * (1.0 + variables.AIRiskDeficitFactor*(1.0-trust))
	return math.Round(risk*10000) / 10000
}

// ComputeReporterExponentialImpulse calcule l'impulsion convexe omega_j = (exp(beta * T_sig) - 1) / (exp(beta) - 1).
// Garantit qu'un signaleur à très haut TrustScore pèse exponentiellement plus lourd qu'un signaleur à bas TrustScore.
func ComputeReporterExponentialImpulse(reporterTrustScore float64) float64 {
	t := math.Max(0.0, math.Min(1.0, reporterTrustScore))
	beta := variables.ReportExpSteepness
	if beta <= 0 {
		return t
	}
	denom := math.Exp(beta) - 1.0
	if denom <= 0 {
		return t
	}
	omega := (math.Exp(beta*t) - 1.0) / denom
	return math.Round(omega*10000) / 10000
}

// ComputeAuthorTrustShield calcule le facteur modérateur de confiance de l'auteur :
// Phi_auteur(T_auteur) = (1 + alpha_def * (1 - T_auteur)) / (1 + kappa_bouclier * T_auteur^2).
func ComputeAuthorTrustShield(authorTrustScore float64) float64 {
	t := math.Max(0.0, math.Min(1.0, authorTrustScore))
	numerator := 1.0 + variables.AIRiskDeficitFactor*(1.0-t)
	denominator := 1.0 + variables.AuthorTrustShieldFactor*(t*t)
	return numerator / denominator
}

// ComputeAccumulatedReporterScore calcule S_sig = W_sig * (omega_max + gamma_log * ln(1 + sum(omega_j))).
func ComputeAccumulatedReporterScore(targetType int, maxOmega float64, sumOmega float64) float64 {
	if maxOmega <= 0 && sumOmega <= 0 {
		return 0.0
	}
	wSig := variables.ModerationWeightSignalPost
	if targetType == variables.ReportTargetMessage {
		wSig = variables.ModerationWeightSignalMsg
	}
	nonLinearTerm := maxOmega + variables.ReportLogDampingFactor*math.Log(1.0+math.Max(0.0, sumOmega))
	return math.Round((wSig*nonLinearTerm)*10000) / 10000
}

// ComputeGlobalModerationPriority calcule la priorité finale P_file dans la file des modérateurs :
// P_file = Phi_auteur(T_auteur) * (W_ia * P_ia + S_sig + B_eco)
func ComputeGlobalModerationPriority(
	iaScoreMax float64,
	authorTrustScore float64,
	accumulatedReportScore float64,
	economicValue float64,
) float64 {
	phiAuthor := ComputeAuthorTrustShield(authorTrustScore)
	iaComponent := variables.ModerationWeightIA * AggregateMaxIAScore(iaScoreMax)
	ecoBonus := 0.0
	if economicValue > 0 {
		ecoBonus = 0.10 * math.Log(1.0+economicValue)
	}
	priority := phiAuthor * (iaComponent + math.Max(0.0, accumulatedReportScore) + ecoBonus)
	return math.Round(priority*10000) / 10000
}

// RecordReporterAndComputePriority enregistre un signaleur humain dans l'accumulateur Redis L1
// et retourne l'état agrégé mis à jour ainsi que la priorité globale P_file.
func RecordReporterAndComputePriority(
	ctx context.Context,
	targetType int,
	targetID int64,
	targetOwnerID int64,
	authorTrustScore float64,
	iaScoreMax float64,
	reporterID int64,
	reporterTrustScore float64,
	economicValue float64,
) (TargetReportAggregation, float64) {
	aggKey := fmt.Sprintf("%d:%d", targetType, targetID)
	var agg TargetReportAggregation
	if redis.ReportAggregators != nil {
		_ = redis.ReportAggregators.GetObject(ctx, aggKey, &agg)
	}

	nowMs := domain.NowMillis()
	if agg.ReportID == 0 {
		agg = TargetReportAggregation{
			ReportID:        pkg.GenerateID(),
			TargetType:      targetType,
			TargetID:        targetID,
			TargetOwnerID:   targetOwnerID,
			ReportOrigin:    variables.ReportOriginUser,
			IAScoreMax:      iaScoreMax,
			ReporterWeights: make(map[int64]float64),
			CreatedAt:       nowMs,
		}
	} else {
		// Si le ticket avait été ouvert par le Robot IA, il passe en mode HYBRIDE (Auto_AI + Utilisateur)
		if agg.ReportOrigin == variables.ReportOriginAutoAI {
			agg.ReportOrigin = variables.ReportOriginHybrid
		}
		if iaScoreMax > agg.IAScoreMax {
			agg.IAScoreMax = iaScoreMax
		}
	}

	if agg.ReporterWeights == nil {
		agg.ReporterWeights = make(map[int64]float64)
	}

	omega := ComputeReporterExponentialImpulse(reporterTrustScore)
	agg.ReporterWeights[reporterID] = omega

	// Recalcul exact sans doublon de signaleur (idempotence par reporterID)
	sumOmega := 0.0
	maxOmega := 0.0
	for _, w := range agg.ReporterWeights {
		sumOmega += w
		if w > maxOmega {
			maxOmega = w
		}
	}
	agg.SumOmega = sumOmega
	agg.MaxOmega = maxOmega
	agg.ReportCount = len(agg.ReporterWeights)

	if redis.ReportAggregators != nil {
		_ = redis.ReportAggregators.SetObject(ctx, aggKey, agg)
	}

	scoringRes := ComputeMasterModerationPriority(MasterScoringInput{
		TargetType:       targetType,
		Origin:           agg.ReportOrigin,
		Category:         variables.ReportCatAIPornography,
		MaxIAScore:       agg.IAScoreMax,
		HasImageOrPost:   true,
		IsPureText:       false,
		AuthorTrustScore: authorTrustScore,
		EconomicValue:    economicValue,
		MaxReporterOmega: agg.MaxOmega,
		SumReporterOmega: agg.SumOmega,
		AgeHours:         0.0,
	})
	priority := scoringRes.FinalQueueScore
	return agg, priority
}

// EvaluateAndTriggerAutoReport évalue la combinaison (P_ia, T_auteur) lors de la création d'un Post ou d'un Message avec image.
// Si le seuil de risque automatique est franchi, le serveur émet un signalement automatique (ReportOriginAutoAI = 1).
// Retourne (autoReported bool, shouldQuarantinePost bool, priority float64).
func EvaluateAndTriggerAutoReport(
	ctx context.Context,
	authorID int64,
	authorTrustScore float64,
	targetType int,
	targetID int64,
	iaScoreMax float64,
) (bool, bool, float64) {
	cleanIAScore := AggregateMaxIAScore(iaScoreMax)
	if cleanIAScore <= 0 {
		return false, authorTrustScore < variables.TrustThresholdStandard, 0.0
	}

	riskIndex := ComputeAutoAIRiskIndex(cleanIAScore, authorTrustScore)

	threshold := variables.AIAutoReportThreshold
	if targetType == variables.ReportTargetMessage {
		threshold = variables.AIAutoReportMessageThreshold
	}

	shouldQuarantinePost := false
	if targetType == variables.ReportTargetPost {
		if authorTrustScore < variables.TrustThresholdStandard || riskIndex >= variables.AIAutoQuarantineThreshold {
			shouldQuarantinePost = true
		}
	}

	if riskIndex < threshold {
		return false, shouldQuarantinePost, 0.0
	}

	// Le seuil est franchi : création du ticket automatique par le Robot Serveur
	nowMs := domain.NowMillis()
	reportID := pkg.GenerateID()

	// Équation Maîtresse Unifiée (§C.2–C.8)
	scoringRes := ComputeMasterModerationPriority(MasterScoringInput{
		TargetType:       targetType,
		Origin:           variables.ReportOriginAutoAI,
		Category:         variables.ReportCatAIPornography,
		MaxIAScore:       cleanIAScore,
		HasImageOrPost:   true,
		IsPureText:       false,
		AuthorTrustScore: authorTrustScore,
		AgeHours:         0.0,
	})
	priority := scoringRes.FinalQueueScore

	aggKey := fmt.Sprintf("%d:%d", targetType, targetID)
	agg := TargetReportAggregation{
		ReportID:        reportID,
		TargetType:      targetType,
		TargetID:        targetID,
		TargetOwnerID:   authorID,
		ReportOrigin:    variables.ReportOriginAutoAI,
		IAScoreMax:      cleanIAScore,
		ReporterWeights: make(map[int64]float64),
		ReportCount:     1,
		CreatedAt:       nowMs,
	}
	if redis.ReportAggregators != nil {
		_ = redis.ReportAggregators.SetObject(ctx, aggKey, agg)
	}

	autoReason := fmt.Sprintf(
		"Détection automatique IA NSFWJS (P_ia=%.2f, T_auteur=%.2f, R_auto=%.2f)",
		cleanIAScore, authorTrustScore, riskIndex,
	)

	autoReportPayload := report_models.ReportPayload{
		ID:                     reportID,
		ReporterID:             0, // 0 = Robot Serveur Numan
		ReportOrigin:           variables.ReportOriginAutoAI,
		ReportOriginLabel:      variables.ReportOriginLabelAutoAI,
		TargetType:             targetType,
		TargetIDs:              []int64{targetID},
		TargetOwnerID:          authorID,
		AuthorTrustScore:       authorTrustScore,
		IAScoreMax:             cleanIAScore,
		AccumulatedReportScore: 0.0,
		ReportCount:            1,
		Category:               variables.ReportCatAIPornography,
		Reason:                 autoReason,
		Rationale:              "",
		State:                  variables.ReportStatePending,
		Importance:             priority,
		CreatedAt:              nowMs,
		UpdatedAt:              nowMs,
	}

	// Indexation en cache de cycle de vie et dans la file ZSET active (§C.9)
	ticketRec := &moderation_models.ModerationTicketRecord{
		ReportID:         reportID,
		TargetType:       targetType,
		TargetID:         targetID,
		TargetOwnerID:    authorID,
		Category:         variables.ReportCatAIPornography,
		ReportOrigin:     variables.ReportOriginAutoAI,
		State:            variables.ReportStatePending,
		Reason:           autoReason,
		StaticBaseScore:  scoringRes.StaticBaseScore,
		PriorityScore:    priority,
		IsInfiniteUrgent: scoringRes.IsInfiniteUrgent,
		CreatedAt:        nowMs,
		UpdatedAt:        nowMs,
	}
	if redis.ModerationTickets != nil {
		_ = redis.ModerationTickets.SetObject(ctx, reportID, ticketRec)
	}
	if redis.ModerationQueues != nil {
		_ = redis.ModerationQueues.ZAdd(ctx, variables.ModerationQueueActive, priority, fmt.Sprintf("%d", reportID))
	}

	if errQueue := redis.EnqueueDB(ctx, autoReportPayload.ID, 0, redis.EntityReport, redis.ActionCreate, autoReportPayload, redis.TargetPostgres); errQueue != nil {
		numan_log.Error(ctx).
			Err(errQueue).
			Int64("target_id", targetID).
			Int("target_type", targetType).
			Msg("Échec du Write-Behind lors de l'émission du signalement automatique IA")
		return false, shouldQuarantinePost, priority
	}

	numan_log.Info(ctx).
		Entity("report", autoReportPayload.ID).
		Action(numan_log.ActionCreate).
		Int64("target_id", targetID).
		Int("target_type", targetType).
		Int64("author_id", authorID).
		Msg("Signalement automatique IA émis dans la file de modération.")

	return true, shouldQuarantinePost, priority
}
