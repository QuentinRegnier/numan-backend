package report_service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/report_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : CRÉATION DE SIGNALEMENT & PONDÉRATION TRUST SCORE
// ############################################################################

// SubmitReport gère le signalement, calcule le Poids d'Impact (Score_signaleur - Score_cible),
// applique le Shadowban préventif si requis (Impact > 0.40 ou 3 signalements standards),
// et envoie le ticket aux workers pour persistance dans PostgreSQL.
func SubmitReport(ctx context.Context, callerID int64, input report_models.CreateReportInput) error {
	currentTime := time.Now().UTC()

	// ── ÉTAPE 1 : CALCUL DU SCORE D'URGENCE ÉCONOMIQUE & TRUST IMPACT (O(1)) ─
	err := pkg.ListLimitVerif(input.TargetIDs, variables.MaxReportTargets)
	if err != nil {
		return err
	}

	economicValueScore := calculateEconomicImportance(ctx, input)

	reporterScore := variables.TrustScoreInitial
	if repLite, errLite := cache_service.GetUserLite(ctx, callerID); errLite == nil && repLite.TrustScore > 0 {
		reporterScore = repLite.TrustScore
	}

	var (
		finalPriority          float64
		primaryTargetOwnerID   int64
		primaryAuthorTrust     = variables.TrustScoreInitial
		maxTargetIAScore       float64
		accumulatedReportScore float64
		aggregatedReportCount  = 1
		reportOrigin           = variables.ReportOriginUser
	)

	for _, targetID := range input.TargetIDs {
		targetOwnerID := resolveTargetOwnerID(ctx, input.TargetType, targetID)
		if targetOwnerID == callerID {
			continue // Auto-signalement ignoré
		}
		if primaryTargetOwnerID == 0 {
			primaryTargetOwnerID = targetOwnerID
		}

		authorTrust := variables.TrustScoreInitial
		if ownerLite, errOwner := cache_service.GetUserLite(ctx, targetOwnerID); errOwner == nil && ownerLite.TrustScore > 0 {
			authorTrust = ownerLite.TrustScore
		}
		primaryAuthorTrust = authorTrust

		targetIAScore := resolveTargetIAScore(ctx, input.TargetType, targetID)
		if targetIAScore > maxTargetIAScore {
			maxTargetIAScore = targetIAScore
		}

		// 1. Agrégation non-linéaire (Exponentielle signaleur + Amortissement Logarithmique + Bouclier Auteur + Score IA)
		agg, computedPriority := moderation_service.RecordReporterAndComputePriority(
			ctx,
			input.TargetType,
			targetID,
			targetOwnerID,
			authorTrust,
			targetIAScore,
			callerID,
			reporterScore,
			economicValueScore,
		)
		accScore := moderation_service.ComputeAccumulatedReporterScore(input.TargetType, agg.MaxOmega, agg.SumOmega)
		if accScore > accumulatedReportScore {
			accumulatedReportScore = accScore
		}
		if agg.ReportCount > aggregatedReportCount {
			aggregatedReportCount = agg.ReportCount
		}
		if agg.ReportOrigin == variables.ReportOriginHybrid {
			reportOrigin = variables.ReportOriginHybrid
		}
		if computedPriority > finalPriority {
			finalPriority = computedPriority
		}
	}

	// Équation Maîtresse Unifiée (§C.2–C.8)
	masterInput := moderation_service.MasterScoringInput{
		TargetType:       input.TargetType,
		Origin:           reportOrigin,
		Category:         input.Category,
		MaxIAScore:       maxTargetIAScore,
		HasImageOrPost:   input.TargetType == variables.ReportTargetPost || maxTargetIAScore > 0,
		IsPureText:       input.TargetType != variables.ReportTargetPost && maxTargetIAScore == 0,
		AuthorTrustScore: primaryAuthorTrust,
		ReporterTrusts:   []float64{reporterScore},
		EconomicValue:    economicValueScore,
		AgeHours:         0.0,
	}
	masterResult := moderation_service.ComputeMasterModerationPriority(masterInput)
	finalPriority = masterResult.FinalQueueScore

	// ── ÉTAPE 2 : CONSTRUCTION DU PAYLOAD DE SIGNALEMENT ENRICHI ─────────────
	reportID := pkg.GenerateID()
	nowMs := domain.TimeToMillis(currentTime)

	reportPayload := report_models.ReportPayload{
		ID:                     reportID,
		ReporterID:             callerID,
		ReportOrigin:           reportOrigin,
		ReportOriginLabel:      report_models.ResolveOriginLabel(reportOrigin),
		TargetType:             input.TargetType,
		TargetIDs:              input.TargetIDs,
		TargetOwnerID:          primaryTargetOwnerID,
		AuthorTrustScore:       primaryAuthorTrust,
		IAScoreMax:             maxTargetIAScore,
		AccumulatedReportScore: accumulatedReportScore,
		ReportCount:            aggregatedReportCount,
		Category:               input.Category,
		Reason:                 pkg.CleanStr(input.Reason),
		Rationale:              "", // Champ réservé pour les notes et décisions des modérateurs
		State:                  variables.ReportStatePending,
		Importance:             finalPriority,
		CreatedAt:              nowMs,
		UpdatedAt:              nowMs,
	}

	// Indexation en cache de cycle de vie et dans la file ZSET active (§C.9)
	ticketRecord := &moderation_models.ModerationTicketRecord{
		ReportID:         reportID,
		TargetType:       input.TargetType,
		TargetID:         targetIDFirst(input.TargetIDs),
		TargetOwnerID:    primaryTargetOwnerID,
		Category:         input.Category,
		ReportOrigin:     reportOrigin,
		State:            variables.ReportStatePending,
		Reason:           pkg.CleanStr(input.Reason),
		StaticBaseScore:  masterResult.StaticBaseScore,
		PriorityScore:    finalPriority,
		IsInfiniteUrgent: masterResult.IsInfiniteUrgent,
		ReporterIDs:      []int64{callerID},
		CreatedAt:        nowMs,
		UpdatedAt:        nowMs,
	}
	_ = cache_service.SaveModerationTicket(ctx, ticketRecord)
	_ = cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueueActive, reportID, finalPriority)

	// Masquage préventif immédiat (Shadowban / Quarantaine) si la publication fait l'objet d'un signalement à fort impact ou urgent
	if input.TargetType == variables.ReportTargetPost {
		isHighImpact := (reporterScore - primaryAuthorTrust) > 0.40
		if masterResult.IsInfiniteUrgent || isHighImpact || finalPriority >= 10.0 {
			for _, targetID := range input.TargetIDs {
				moderation_service.ApplyPreventiveShadowbanToPost(ctx, targetID)
			}
		}
	}

	// ── ÉTAPE 3 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────

	errQueue := redis.EnqueueDB(ctx, reportPayload.ID, 0, redis.EntityReport, redis.ActionCreate, reportPayload, redis.TargetPostgres)

	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("reporter_id", callerID).Msg("Échec du Write-Behind lors de la création d'un signalement")
		return numan_error.NewInternal()
	}

	return nil
}

func targetIDFirst(ids []int64) int64 {
	if len(ids) > 0 {
		return ids[0]
	}
	return 0
}
