package moderation_service

import (
	"context"
	"fmt"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/report_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ClaimTicketService permet à un modérateur de réclamer un ticket Pending pour traitement exclusif (verrou 48h).
func ClaimTicketService(ctx context.Context, modID int64, reportID int64) (*moderation_models.ModerationTicketPublicView, error) {
	ticket, err := cache_service.GetModerationTicket(ctx, reportID)
	if err != nil {
		return nil, err
	}
	if ticket.State != variables.ReportStatePending {
		return nil, numan_error.NewConflict(numan_error.CodeConflict, "Ce ticket n'est pas en attente de traitement.", nil)
	}

	nowMs := domain.NowMillis()
	lockExpiresAtMs := nowMs + int64(variables.ModerationLockTTL.Milliseconds())

	acquired, errLock := cache_service.AcquireTicketLock(ctx, reportID, modID, lockExpiresAtMs, variables.ModerationLockTTL)
	if errLock != nil {
		return nil, errLock
	}
	if !acquired {
		return nil, numan_error.NewConflict(numan_error.CodeConflict, "Ce ticket est déjà pris en charge par un autre modérateur.", nil)
	}

	_ = cache_service.RemoveTicketFromModerationQueue(ctx, variables.ModerationQueueActive, reportID)

	ticket.State = variables.ReportStateInProgress
	ticket.ClaimedByModID = modID
	ticket.LockExpiresAtMs = lockExpiresAtMs
	ticket.UpdatedAt = nowMs

	if errSave := cache_service.SaveModerationTicket(ctx, ticket); errSave != nil {
		_ = cache_service.ReleaseTicketLock(ctx, reportID)
		return nil, errSave
	}

	numan_log.Info(ctx).
		Entity("report", reportID).
		Int64("mod_id", modID).
		Msg("Ticket de modération réclamé avec succès (verrou exclusif 48h posé).")

	view := ToPublicTicketView(ticket, false)
	return &view, nil
}

// ReleaseTicketService permet au modérateur détenteur de libérer volontairement un ticket InProgress et le remettre en Pending.
func ReleaseTicketService(ctx context.Context, modID int64, modGrade int, reportID int64) (*moderation_models.ModerationTicketPublicView, error) {
	ticket, err := cache_service.GetModerationTicket(ctx, reportID)
	if err != nil {
		return nil, err
	}
	if ticket.State != variables.ReportStateInProgress {
		return nil, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le ticket n'est pas en cours de traitement.", nil)
	}
	if ticket.ClaimedByModID != modID && modGrade != variables.UserGradeAdmin {
		return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous n'êtes pas le modérateur assigné à ce ticket.", nil)
	}

	_ = cache_service.ReleaseTicketLock(ctx, reportID)

	ticket.State = variables.ReportStatePending
	ticket.ClaimedByModID = 0
	ticket.LockExpiresAtMs = 0
	ticket.UpdatedAt = domain.NowMillis()

	_ = cache_service.SaveModerationTicket(ctx, ticket)
	_ = cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueueActive, reportID, ticket.PriorityScore)

	numan_log.Info(ctx).
		Entity("report", reportID).
		Int64("mod_id", modID).
		Msg("Ticket de modération libéré et réintégré dans la file active.")

	view := ToPublicTicketView(ticket, false)
	return &view, nil
}

// RequestTicketInfoService gèle un ticket InProgress en attente d'un complément d'information du plaignant (minuteur 7j).
func RequestTicketInfoService(ctx context.Context, modID int64, modGrade int, input moderation_models.RequestTicketInfoInput) (*moderation_models.ModerationTicketPublicView, error) {
	ticket, err := cache_service.GetModerationTicket(ctx, input.ReportID)
	if err != nil {
		return nil, err
	}
	if ticket.State != variables.ReportStateInProgress {
		return nil, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le ticket doit être en cours de traitement pour demander un complément.", nil)
	}
	if ticket.ClaimedByModID != modID && modGrade != variables.UserGradeAdmin {
		return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous n'êtes pas le modérateur assigné à ce ticket.", nil)
	}

	nowMs := domain.NowMillis()
	waitingUntilMs := nowMs + int64(variables.ModerationWaitingInfoTTL.Milliseconds())

	// Libération du verrou InProgress et enregistrement de l'échéance de 7 jours
	_ = cache_service.ReleaseTicketLock(ctx, input.ReportID)
	_ = cache_service.ScheduleTicketWaitingInfoExpiration(ctx, input.ReportID, waitingUntilMs)

	ticket.State = variables.ReportStateWaitingForInfo
	ticket.InfoRequestMessage = pkg.CleanStr(input.Question)
	ticket.WaitingUntilMs = waitingUntilMs
	ticket.UpdatedAt = nowMs

	if errSave := cache_service.SaveModerationTicket(ctx, ticket); errSave != nil {
		return nil, errSave
	}

	view := ToPublicTicketView(ticket, false)
	return &view, nil
}

// ReplyTicketInfoService permet au plaignant de répondre, ce qui réactive immédiatement le ticket en InProgress pour le même modérateur.
func ReplyTicketInfoService(ctx context.Context, callerID int64, callerGrade int, input moderation_models.ReplyTicketInfoInput) (*moderation_models.ModerationTicketPublicView, error) {
	ticket, err := cache_service.GetModerationTicket(ctx, input.ReportID)
	if err != nil {
		return nil, err
	}
	if ticket.State != variables.ReportStateWaitingForInfo {
		return nil, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le ticket n'est pas en attente d'information.", nil)
	}

	isReporter := false
	for _, repID := range ticket.ReporterIDs {
		if repID == callerID {
			isReporter = true
			break
		}
	}
	if !isReporter && callerGrade != variables.UserGradeAdmin {
		return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Seul le plaignant à l'origine du signalement peut répondre à cette demande.", nil)
	}

	nowMs := domain.NowMillis()
	lockExpiresAtMs := nowMs + int64(variables.ModerationLockTTL.Milliseconds())

	_ = cache_service.ClearTicketWaitingInfoExpiration(ctx, input.ReportID)
	if ticket.ClaimedByModID > 0 {
		_, _ = cache_service.AcquireTicketLock(ctx, input.ReportID, ticket.ClaimedByModID, lockExpiresAtMs, variables.ModerationLockTTL)
	}

	ticket.State = variables.ReportStateInProgress
	ticket.InfoReplyMessage = pkg.CleanStr(input.Reply)
	ticket.WaitingUntilMs = 0
	ticket.LockExpiresAtMs = lockExpiresAtMs
	ticket.UpdatedAt = nowMs

	if errSave := cache_service.SaveModerationTicket(ctx, ticket); errSave != nil {
		return nil, errSave
	}

	view := ToPublicTicketView(ticket, false)
	return &view, nil
}

// ProposeBlameService initie la procédure de blâme Four-Eyes (Modérateur A -> PendingPeerReview avec pseudonyme HMAC).
func ProposeBlameService(ctx context.Context, modID int64, modGrade int, input moderation_models.ProposeBlameInput) (*moderation_models.ModerationTicketPublicView, error) {
	ticket, err := cache_service.GetModerationTicket(ctx, input.ReportID)
	if err != nil {
		return nil, err
	}
	if ticket.State != variables.ReportStateInProgress {
		return nil, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le ticket doit être en cours de traitement pour proposer un blâme.", nil)
	}
	if ticket.ClaimedByModID != modID && modGrade != variables.UserGradeAdmin {
		return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous n'êtes pas le modérateur assigné à ce ticket.", nil)
	}

	nowMs := domain.NowMillis()

	_ = cache_service.ReleaseTicketLock(ctx, input.ReportID)

	ticket.State = variables.ReportStatePendingPeerReview
	ticket.ProposerModID = modID
	ticket.ProposerPseudonym = ""
	ticket.BlameMotivation = pkg.CleanStr(input.Motivation)
	ticket.LockExpiresAtMs = 0
	ticket.UpdatedAt = nowMs

	if errSave := cache_service.SaveModerationTicket(ctx, ticket); errSave != nil {
		return nil, errSave
	}

	// Indexation dans la sous-file de validation croisée
	_ = cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueuePeerReview, ticket.ReportID, ticket.PriorityScore)

	numan_log.Info(ctx).
		Entity("report", input.ReportID).
		Msg("Proposition de blâme initiée (Four-Eyes en attente).")

	view := ToPublicTicketView(ticket, false)
	return &view, nil
}

// ReviewBlameProposalService permet au Modérateur B de valider ou contredire la proposition de blâme sans connaître l'identité de Mod A.
func ReviewBlameProposalService(ctx context.Context, modID int64, modGrade int, input moderation_models.ReviewBlameInput) (*moderation_models.ModerationTicketPublicView, error) {
	ticket, err := cache_service.GetModerationTicket(ctx, input.ReportID)
	if err != nil {
		return nil, err
	}
	if ticket.State != variables.ReportStatePendingPeerReview {
		return nil, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le ticket n'est pas en attente de revue par un pair.", nil)
	}

	// INTERDICTION ABSOLUE D'AUTO-VALIDATION (§C.10.2) : L'erreur NE DOIT PAS révéler l'identité de Mod A
	if ticket.ProposerModID == modID && modGrade != variables.UserGradeAdmin {
		return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Le modérateur proposant ne peut pas valider sa propre proposition de blâme.", nil)
	}

	nowMs := domain.NowMillis()
	_ = cache_service.RemoveTicketFromModerationQueue(ctx, variables.ModerationQueuePeerReview, input.ReportID)

	// CAS 1 : CONSENSUS ATTEINT (Validation du blâme)
	if input.Decision == "validate" {
		targetStats, errStats := cache_service.GetOrInitModeratorStats(ctx, ticket.TargetOwnerID, 0)
		if errStats != nil {
			return nil, errStats
		}

		newBlame := moderation_models.BlameRecord{
			ReportID:       ticket.ReportID,
			TargetType:     ticket.TargetType,
			TargetID:       ticket.TargetID,
			ProposerModID:  ticket.ProposerModID,
			ValidatorModID: modID,
			Motivation:     ticket.BlameMotivation,
			CreatedAt:      nowMs,
		}
		targetStats.BlameHistory = append(targetStats.BlameHistory, newBlame)
		targetStats.BlameCount++
		targetStats.UpdatedAt = nowMs
		_ = cache_service.SaveModeratorStats(ctx, targetStats)

		// Sanction sur le contenu cible (Soft-Delete) et sur le Trust Score de l'auteur
		applyContentSoftDelete(ctx, ticket.TargetType, ticket.TargetID)
		_ = PublishTrustScoreEvent(ctx, ticket.TargetOwnerID, variables.TrustTriggerMinorInfraction, ticket.TargetID, "Blâme confirmé par consensus Four-Eyes")

		// AUDIT DES 3 BLÂMES (§C.11) : Si le quota est atteint, émettre un Super-Report Système en Escalated
		if targetStats.BlameCount == variables.BlameQuotaForAudit {
			triggerThreeBlameSystemAudit(ctx, ticket.TargetOwnerID, targetStats.BlameHistory)
		}

		ticket.State = variables.ReportStateClosed
		ticket.ReviewerModID = modID
		ticket.ReviewerRationale = pkg.CleanStr(input.Rationale)
		ticket.ResolutionOutcome = "blame_validated"
		ticket.UpdatedAt = nowMs

		_ = cache_service.SaveModerationTicket(ctx, ticket)
		_ = postgres.FuncArchiveClosedReport(ctx, ticket.ReportID, variables.ReportStateClosed, fmt.Sprintf("Blâme validé par consensus modérateur : %s", ticket.BlameMotivation))

		// Récompense les signaleurs initiaux
		for _, repID := range ticket.ReporterIDs {
			_ = PublishTrustScoreEvent(ctx, repID, variables.TrustTriggerJustifiedReport, ticket.TargetID, "Signalement justifié validé par modération")
		}

		view := ToPublicTicketView(ticket, false)
		return &view, nil
	}

	// CAS 2 : CONTRADICTION (Modérateur B s'oppose au blâme)
	ticket.ReviewerModID = modID
	ticket.ReviewerRationale = pkg.CleanStr(input.Rationale)
	ticket.EscalationType = "blame_contradiction"
	ticket.UpdatedAt = nowMs

	delegationEnabled := variables.ModerationAdminDelegationEnabled
	if delegationEnabled {
		// Délégation active : redirection vers la file Super-Modérateurs (T_mod > 0.90)
		ticket.State = variables.ReportStatePendingFinalVote
		_ = cache_service.SaveModerationTicket(ctx, ticket)
		_ = cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueueFinalVote, ticket.ReportID, ticket.PriorityScore)
		numan_log.Warn(ctx).Entity("report", ticket.ReportID).Msg("Contradiction de blâme : déléguée au vote final des Super-Modérateurs.")
	} else {
		// Délégation inactive : escalade directe vers les Administrateurs (Grade 4)
		ticket.State = variables.ReportStateEscalated
		_ = cache_service.SaveModerationTicket(ctx, ticket)
		_ = cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueueAdmin, ticket.ReportID, ticket.PriorityScore)
		numan_log.Warn(ctx).Entity("report", ticket.ReportID).Msg("Contradiction de blâme : escaladée à la file Administrateur.")
	}

	view := ToPublicTicketView(ticket, false)
	return &view, nil
}

// ResolveFinalVoteService permet à un Super-Modérateur indépendant (T_mod > 0.90) ou à un Admin de trancher une contradiction.
func ResolveFinalVoteService(ctx context.Context, modID int64, modGrade int, input moderation_models.FinalVoteBlameInput) (*moderation_models.ModerationTicketPublicView, error) {
	ticket, err := cache_service.GetModerationTicket(ctx, input.ReportID)
	if err != nil {
		return nil, err
	}
	if ticket.State != variables.ReportStatePendingFinalVote && ticket.State != variables.ReportStateEscalated {
		return nil, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Ce ticket n'est pas en attente de vote d'arbitrage final.", nil)
	}

	// Mod C doit impérativement être distinct de Mod A et Mod B
	if (ticket.ProposerModID == modID || ticket.ReviewerModID == modID) && modGrade != variables.UserGradeAdmin {
		return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "L'arbitre du vote final doit être un modérateur tiers indépendant.", nil)
	}

	// Vérification de la qualification Super-Modérateur (T_mod > 0.90)
	modStats, errStats := cache_service.GetOrInitModeratorStats(ctx, modID, modGrade)
	if errStats != nil {
		return nil, errStats
	}
	if modStats.ModeratorTrustScore <= variables.SuperModeratorTrustThreshold && modGrade != variables.UserGradeAdmin {
		return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Seul un Super-Modérateur (fiabilité > 0.90) ou un Administrateur peut voter l'arbitrage final.", nil)
	}

	nowMs := domain.NowMillis()
	_ = cache_service.RemoveTicketFromModerationQueue(ctx, variables.ModerationQueueFinalVote, input.ReportID)
	_ = cache_service.RemoveTicketFromModerationQueue(ctx, variables.ModerationQueueAdmin, input.ReportID)

	ticket.FinalVoterModID = modID
	ticket.State = variables.ReportStateClosed
	ticket.ResolutionOutcome = input.Decision
	ticket.UpdatedAt = nowMs

	if input.Decision == "confirm_blame" {
		targetStats, _ := cache_service.GetOrInitModeratorStats(ctx, ticket.TargetOwnerID, 0)
		targetStats.BlameHistory = append(targetStats.BlameHistory, moderation_models.BlameRecord{
			ReportID:        ticket.ReportID,
			TargetType:      ticket.TargetType,
			TargetID:        ticket.TargetID,
			ProposerModID:   ticket.ProposerModID,
			ValidatorModID:  ticket.ReviewerModID,
			FinalVoterModID: modID,
			Motivation:      ticket.BlameMotivation,
			CreatedAt:       nowMs,
		})
		targetStats.BlameCount++
		targetStats.UpdatedAt = nowMs
		_ = cache_service.SaveModeratorStats(ctx, targetStats)

		applyContentSoftDelete(ctx, ticket.TargetType, ticket.TargetID)
		_ = PublishTrustScoreEvent(ctx, ticket.TargetOwnerID, variables.TrustTriggerMinorInfraction, ticket.TargetID, "Blâme confirmé par arbitrage final")

		if targetStats.BlameCount == variables.BlameQuotaForAudit {
			triggerThreeBlameSystemAudit(ctx, ticket.TargetOwnerID, targetStats.BlameHistory)
		}
		_ = postgres.FuncArchiveClosedReport(ctx, ticket.ReportID, variables.ReportStateClosed, fmt.Sprintf("Blâme confirmé lors du vote final : %s", input.Rationale))
	} else {
		// Rejet du blâme : clôture sans pénalité sur la cible
		_ = postgres.FuncArchiveClosedReport(ctx, ticket.ReportID, variables.ReportStateClosed, fmt.Sprintf("Blâme rejeté lors du vote final : %s", input.Rationale))
	}

	_ = cache_service.SaveModerationTicket(ctx, ticket)

	view := ToPublicTicketView(ticket, false)
	return &view, nil
}

// EscalateTicketService escalade un ticket InProgress vers la file Administrateur pour hard ban ou clarification CGU.
func EscalateTicketService(ctx context.Context, modID int64, modGrade int, input moderation_models.EscalateTicketInput) (*moderation_models.ModerationTicketPublicView, error) {
	ticket, err := cache_service.GetModerationTicket(ctx, input.ReportID)
	if err != nil {
		return nil, err
	}
	if ticket.State != variables.ReportStateInProgress {
		return nil, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le ticket doit être en cours de traitement pour être escaladé.", nil)
	}
	if ticket.ClaimedByModID != modID && modGrade != variables.UserGradeAdmin {
		return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous n'êtes pas le modérateur assigné à ce ticket.", nil)
	}

	nowMs := domain.NowMillis()

	_ = cache_service.ReleaseTicketLock(ctx, input.ReportID)

	ticket.State = variables.ReportStateEscalated
	ticket.EscalationType = input.EscalationType
	ticket.EscalationReason = pkg.CleanStr(input.Reason)
	ticket.CGUFeedbackNote = pkg.CleanStr(input.CGUFeedbackNote)
	ticket.LockExpiresAtMs = 0
	ticket.UpdatedAt = nowMs

	if errSave := cache_service.SaveModerationTicket(ctx, ticket); errSave != nil {
		return nil, errSave
	}

	_ = cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueueAdmin, ticket.ReportID, ticket.PriorityScore)

	numan_log.Warn(ctx).
		Entity("report", input.ReportID).
		Str("type", input.EscalationType).
		Msg("Ticket escaladé avec succès vers la file Administrateur.")

	view := ToPublicTicketView(ticket, true)
	return &view, nil
}

// CloseTicketService clôture un ticket, enregistre l'archivage en base froide et envoie le feedback réputationnel aux signaleurs.
func CloseTicketService(ctx context.Context, _ int64, isDismiss bool, rationale string, reportID int64) (*moderation_models.CloseTicketOutput, error) {
	ticket, err := cache_service.GetModerationTicket(ctx, reportID)
	if err != nil {
		return nil, err
	}
	nowMs := domain.NowMillis()

	ticket.State = variables.ReportStateClosed
	ticket.ReviewerRationale = pkg.CleanStr(rationale)
	ticket.UpdatedAt = nowMs

	outcome := "confirmed"
	if isDismiss {
		outcome = "dismissed"
		ticket.ResolutionOutcome = outcome
		for _, repID := range ticket.ReporterIDs {
			_ = PublishTrustScoreEvent(ctx, repID, variables.TrustTriggerAbusiveReport, ticket.TargetID, "Signalement rejeté comme infondé ou abusif")
		}
	} else {
		ticket.ResolutionOutcome = outcome
		for _, repID := range ticket.ReporterIDs {
			_ = PublishTrustScoreEvent(ctx, repID, variables.TrustTriggerJustifiedReport, ticket.TargetID, "Signalement justifié confirmé par modérateur")
		}
	}

	_ = cache_service.SaveModerationTicket(ctx, ticket)
	_ = postgres.FuncArchiveClosedReport(ctx, ticket.ReportID, variables.ReportStateClosed, rationale)
	_ = cache_service.RemoveTicketFromAllModerationQueues(ctx, ticket.ReportID)
	_ = cache_service.ReleaseTicketLock(ctx, ticket.ReportID)
	_ = cache_service.ClearTicketWaitingInfoExpiration(ctx, ticket.ReportID)

	return &moderation_models.CloseTicketOutput{
		ReportID:   ticket.ReportID,
		State:      variables.ReportStateClosed,
		Outcome:    outcome,
		ArchivedAt: nowMs,
	}, nil
}

// triggerThreeBlameSystemAudit génère un Super-Report Système escaladé en file Administrateur lors du 3ème blâme.
func triggerThreeBlameSystemAudit(ctx context.Context, targetUserID int64, history []moderation_models.BlameRecord) {
	nowMs := domain.NowMillis()
	superReportID := pkg.GenerateID()

	superReport := &moderation_models.ModerationTicketRecord{
		ReportID:          superReportID,
		TargetType:        variables.ReportTargetUser,
		TargetID:          targetUserID,
		TargetOwnerID:     targetUserID,
		Category:          variables.ReportCatOther,
		ReportOrigin:      variables.ReportOriginSystem,
		State:             variables.ReportStateEscalated,
		Reason:            "Quota de 3 blâmes consécutifs atteint (Audit Administrateur requis)",
		StaticBaseScore:   100.0,
		PriorityScore:     100.0,
		EscalationType:    "blame_quota_audit",
		AuditBlameRecords: history,
		CreatedAt:         nowMs,
		UpdatedAt:         nowMs,
	}

	_ = cache_service.SaveModerationTicket(ctx, superReport)
	_ = cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueueAdmin, superReportID, superReport.PriorityScore)

	reportPayload := report_models.ReportPayload{
		ID:                superReportID,
		ReporterID:        0,
		ReportOrigin:      variables.ReportOriginSystem,
		ReportOriginLabel: variables.ReportOriginLabelSystem,
		TargetType:        variables.ReportTargetUser,
		TargetIDs:         []int64{targetUserID},
		TargetOwnerID:     targetUserID,
		Category:          variables.ReportCatOther,
		Reason:            superReport.Reason,
		State:             variables.ReportStateEscalated,
		Importance:        100.0,
		CreatedAt:         nowMs,
		UpdatedAt:         nowMs,
	}
	_ = redis.EnqueueDB(ctx, superReportID, 0, redis.EntityReport, redis.ActionCreate, reportPayload, redis.TargetPostgres)

	numan_log.Warn(ctx).
		Int64("super_report_id", superReportID).
		Int64("target_user_id", targetUserID).
		Msg("ALERTE SYSTÈME : Quota de 3 blâmes atteint. Super-Report d'audit Administrateur généré.")
}

func applyContentSoftDelete(ctx context.Context, targetType int, targetID int64) {
	if targetType == variables.ReportTargetPost {
		post, err := object_cache_service.GetPostFromObjectCache(ctx, targetID)
		if err == nil {
			post.Visibility = variables.PostVisibilitySoftDeleted
			_ = object_cache_service.SetPostInObjectCache(ctx, post)
		}
	}
}
