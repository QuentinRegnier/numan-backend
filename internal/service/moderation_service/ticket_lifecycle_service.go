package moderation_service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
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

// ClockFunc permet d'injecter une horloge personnalisée pour tester de manière déterministe le cycle de vie.
type LifecycleClockFunc func() time.Time

var lifecycleClock LifecycleClockFunc = time.Now

// SetLifecycleClock injecte une horloge personnalisée (utilisé pour les tests).
func SetLifecycleClock(c LifecycleClockFunc) func() {
	prev := lifecycleClock
	if c == nil {
		lifecycleClock = time.Now
	} else {
		lifecycleClock = c
	}
	return func() {
		lifecycleClock = prev
	}
}

func getLifecycleTime() time.Time {
	if lifecycleClock != nil {
		return lifecycleClock()
	}
	return time.Now()
}

// ClaimTicketService permet à un modérateur de réclamer un ticket Pending pour traitement exclusif (verrou 48h).
func ClaimTicketService(ctx context.Context, modID int64, reportID int64) (*moderation_models.ModerationTicketPublicView, error) {
	ticket, err := cache_service.GetModerationTicket(ctx, reportID)
	if err != nil {
		return nil, err
	}
	if ticket.State != variables.ReportStatePending {
		return nil, numan_error.NewConflict(numan_error.CodeConflict, "Ce ticket n'est pas en attente de traitement.", nil)
	}

	now := getLifecycleTime()
	nowMs := now.UnixMilli()
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
	ticket.UpdatedAt = getLifecycleTime().UnixMilli()

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

	now := getLifecycleTime()
	nowMs := now.UnixMilli()
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

	now := getLifecycleTime()
	nowMs := now.UnixMilli()
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

	now := getLifecycleTime()
	nowMs := now.UnixMilli()

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

	now := getLifecycleTime()
	nowMs := now.UnixMilli()
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

	now := getLifecycleTime()
	nowMs := now.UnixMilli()
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

	now := getLifecycleTime()
	nowMs := now.UnixMilli()

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

// ResolveAdminAuditService permet à un Administrateur (Grade 4) de trancher l'audit des 3 blâmes.
func ResolveAdminAuditService(ctx context.Context, adminID int64, adminGrade int, input moderation_models.ResolveAdminAuditInput) (*moderation_models.ResolveAdminAuditOutput, error) {
	if adminGrade != variables.UserGradeAdmin {
		return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Seul un Administrateur (Grade 4) peut statuer sur un audit de blâmes.", nil)
	}

	ticket, err := cache_service.GetModerationTicket(ctx, input.ReportID)
	if err != nil {
		return nil, err
	}
	if ticket.State != variables.ReportStateEscalated || ticket.EscalationType != "blame_quota_audit" {
		return nil, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Ce ticket n'est pas un audit de quota de 3 blâmes en attente.", nil)
	}

	now := getLifecycleTime()
	nowMs := now.UnixMilli()
	_ = cache_service.RemoveTicketFromModerationQueue(ctx, variables.ModerationQueueAdmin, ticket.ReportID)

	out := &moderation_models.ResolveAdminAuditOutput{}

	if input.Decision == "validate" {
		// DÉCISION FATALE : Confirmation des 3 blâmes -> Bannissement définitif (Hard Ban)
		_, errBan := ExecuteAdminHardBanService(ctx, adminID, moderation_models.AdminHardBanInput{
			TargetUserID:    ticket.TargetOwnerID,
			ReportID:        ticket.ReportID,
			Reason:          fmt.Sprintf("Confirmation de l'audit des 3 blâmes : %s", input.Rationale),
			CGUFeedbackNote: "Sanction validée par l'Administration suite à 3 infractions répétées.",
		})
		if errBan != nil {
			return nil, errBan
		}
		out.TargetBanned = true

		// Récompense des modérateurs ayant instruit les blâmes (+0.02 de T_mod)
		modIDsToReward := make(map[int64]struct{})
		for _, b := range ticket.AuditBlameRecords {
			if b.ProposerModID > 0 {
				modIDsToReward[b.ProposerModID] = struct{}{}
			}
			if b.ValidatorModID > 0 {
				modIDsToReward[b.ValidatorModID] = struct{}{}
			}
		}
		for mID := range modIDsToReward {
			st, errSt := cache_service.GetOrInitModeratorStats(ctx, mID, variables.UserGradeModerator)
			if errSt != nil {
				return nil, errSt
			}
			st.ModeratorTrustScore = clampTrustUnit(st.ModeratorTrustScore + variables.ModTrustValidationBonus)
			if errSave := cache_service.SaveModeratorStats(ctx, st); errSave != nil {
				return nil, errSave
			}
			out.RewardedModIDs = append(out.RewardedModIDs, mID)
		}

		ticket.ResolutionOutcome = "blames_validated_fatal_ban"
	} else {
		// PARDON / RÉVOCATION : Révocation totale ou partielle des blâmes infondés ou abusifs
		revokedSet := make(map[int64]bool)
		if len(input.RevokedBlameReportIDs) > 0 {
			for _, id := range input.RevokedBlameReportIDs {
				revokedSet[id] = true
			}
		} else {
			// Si aucun ID spécifique n'est spécifié, tous les blâmes audités sont révoqués
			for _, b := range ticket.AuditBlameRecords {
				revokedSet[b.ReportID] = true
			}
		}

		// Filtrage des blâmes : on sépare les blâmes révoqués des blâmes conservés
		var retainedBlames []moderation_models.BlameRecord
		modIDsToPenalize := make(map[int64]struct{})

		for _, b := range ticket.AuditBlameRecords {
			if revokedSet[b.ReportID] {
				// Blâme révoqué : on sanctionne uniquement ses deux modérateurs (A et B)
				if b.ProposerModID > 0 {
					modIDsToPenalize[b.ProposerModID] = struct{}{}
				}
				if b.ValidatorModID > 0 {
					modIDsToPenalize[b.ValidatorModID] = struct{}{}
				}
			} else {
				// Blâme maintenu
				retainedBlames = append(retainedBlames, b)
			}
		}

		// Rétablissement ou ajustement du compteur de blâmes de la cible
		targetStats, errStats := cache_service.GetOrInitModeratorStats(ctx, ticket.TargetOwnerID, 0)
		if errStats != nil {
			return nil, errStats
		}
		targetStats.BlameHistory = retainedBlames
		targetStats.BlameCount = len(retainedBlames)
		targetStats.UpdatedAt = nowMs
		if errSave := cache_service.SaveModeratorStats(ctx, targetStats); errSave != nil {
			return nil, errSave
		}

		// Récompense de réputation suite à contestation gagnée (§C.13 : +0.08)
		if len(revokedSet) > 0 && ticket.TargetOwnerID > 0 {
			_ = PublishTrustScoreEvent(
				ctx,
				ticket.TargetOwnerID,
				variables.TrustTriggerSuccessfulAppeal,
				ticket.TargetID,
				"Contestation gagnée : révocation de blâme injustifié par l'Administration",
			)
		}

		// Application de la pénalité sévère T_mod (-0.25) sur les modérateurs des seuls blâmes révoqués
		for mID := range modIDsToPenalize {
			st, errSt := cache_service.GetOrInitModeratorStats(ctx, mID, variables.UserGradeModerator)
			if errSt != nil {
				return nil, errSt
			}
			st.ModeratorTrustScore = clampTrustUnit(st.ModeratorTrustScore - variables.ModTrustFalseBlamePenalty)
			out.PenalizedModIDs = append(out.PenalizedModIDs, mID)

			// Révocation automatique des privilèges si T_mod < 0.50
			if st.ModeratorTrustScore < variables.ModTrustRevocationThreshold {
				st.Grade = variables.UserGradeUser
				out.DemotedModIDs = append(out.DemotedModIDs, mID)
				numan_log.Warn(ctx).
					Int64("demoted_mod_id", mID).
					Float64("t_mod", st.ModeratorTrustScore).
					Msg("Révocation automatique des droits de modérateur suite à blâmes abusifs répétés.")

				// Mise à jour synchrone du cache SPEED L1
				if uLite, errU := cache_service.GetUserLite(ctx, mID); errU == nil && uLite.ID > 0 {
					uLite.Grade = variables.UserGradeUser
					_ = cache_service.UpdateUserLiteInSpeedCache(ctx, uLite)
				}

				// Persistance asynchrone du grade utilisateur via EnqueueDB (PostgreSQL / MongoDB)
				uPayload := auth_models.UserPayload{
					ID:        mID,
					Grade:     variables.UserGradeUser,
					UpdatedAt: nowMs,
				}
				_ = redis.EnqueueDB(ctx, mID, mID, redis.EntityUser, redis.ActionUpdate, uPayload, redis.TargetAll)
			}
			if errSave := cache_service.SaveModeratorStats(ctx, st); errSave != nil {
				return nil, errSave
			}
		}

		if len(retainedBlames) == 0 {
			ticket.ResolutionOutcome = "blames_revoked_pardon"
		} else {
			ticket.ResolutionOutcome = fmt.Sprintf("blames_partially_revoked_%d_retained", len(retainedBlames))
		}
	}

	ticket.State = variables.ReportStateClosed
	ticket.ReviewerRationale = pkg.CleanStr(input.Rationale)
	ticket.UpdatedAt = nowMs
	if errSave := cache_service.SaveModerationTicket(ctx, ticket); errSave != nil {
		return nil, errSave
	}
	_ = postgres.FuncArchiveClosedReport(ctx, ticket.ReportID, variables.ReportStateClosed, fmt.Sprintf("Résolution Audit Administrateur : %s", ticket.ResolutionOutcome))

	out.Ticket = ToPublicTicketView(ticket, true)
	return out, nil
}

// ExecuteAdminHardBanService exécute le bannissement définitif d'un compte (Hard Ban immédiat).
func ExecuteAdminHardBanService(ctx context.Context, adminID int64, input moderation_models.AdminHardBanInput) (*moderation_models.AdminHardBanOutput, error) {
	nowMs := getLifecycleTime().UnixMilli()

	// 1. Mise à 0.00 et marquage du compte banni en cache SPEED L1
	userLite, errLite := cache_service.GetUserLite(ctx, input.TargetUserID)
	if errLite == nil && userLite.ID > 0 {
		userLite.TrustScore = variables.TrustScoreHardBan
		_ = redis.UsersLite.SetObject(ctx, input.TargetUserID, userLite)
	}

	// 2. Révocation immédiate des sessions L1 (Tombstone Blacklist + EnqueueDB Delete)
	sessions, errSess := postgres.FuncLoadAllUserSessions(ctx, input.TargetUserID)
	if errSess == nil {
		for _, sess := range sessions {
			idxKey := fmt.Sprintf("%d:%s", input.TargetUserID, sess.FirebaseInstallationID)
			_ = redis.SessionBlacklist.SetPrimitive(ctx, idxKey, "revoked")
			_ = redis.Expire(ctx, idxKey, 24*time.Hour)
			_ = redis.EnqueueDB(ctx, sess.ID, input.TargetUserID, redis.EntitySession, redis.ActionDelete, sess, redis.TargetAll)
		}
	}

	// 3. Enregistrement du bannissement stéganographique (O(1) Redis + Table PostgreSQL)
	steganoMember := fmt.Sprintf("%d", input.TargetUserID)
	if redis.SteganoBanlist != nil {
		_ = redis.SteganoBanlist.SAdd(ctx, "global", steganoMember)
	}
	_ = postgres.FuncRegisterSteganoBan(ctx, input.TargetUserID, steganoMember, input.Reason, input.ReportID)

	// 4. Publication de l'événement majeur dans le Worker de réputation
	_ = PublishTrustScoreEvent(ctx, input.TargetUserID, variables.TrustTriggerMajorInfraction, input.TargetUserID, input.Reason)

	// 5. Clôture éventuelle du ticket associé avec feedback CGU
	if input.ReportID > 0 {
		if ticket, errTicket := cache_service.GetModerationTicket(ctx, input.ReportID); errTicket == nil {
			ticket.State = variables.ReportStateClosed
			ticket.CGUFeedbackNote = pkg.CleanStr(input.CGUFeedbackNote)
			ticket.ResolutionOutcome = "hard_ban_executed"
			ticket.UpdatedAt = nowMs
			_ = cache_service.SaveModerationTicket(ctx, ticket)
			_ = cache_service.RemoveTicketFromAllModerationQueues(ctx, ticket.ReportID)
			_ = postgres.FuncArchiveClosedReport(ctx, ticket.ReportID, variables.ReportStateClosed, fmt.Sprintf("Hard Ban exécuté par l'Administration : %s", input.Reason))
		}
	}

	numan_log.Warn(ctx).
		Int64("admin_id", adminID).
		Int64("banned_user_id", input.TargetUserID).
		Msg("Bannissement définitif (Hard Ban) exécuté par l'Administration.")

	return &moderation_models.AdminHardBanOutput{
		TargetUserID:    input.TargetUserID,
		Banned:          true,
		TrustScore:      variables.TrustScoreHardBan,
		CGUFeedbackNote: input.CGUFeedbackNote,
		ExecutedAt:      nowMs,
	}, nil
}

// SetDelegationSwitchService active ou désactive l'interrupteur de délégation administrative vers les Super-Modérateurs.
func SetDelegationSwitchService(ctx context.Context, adminGrade int, enabled bool) (*moderation_models.AdminDelegationSwitchOutput, error) {
	if adminGrade != variables.UserGradeAdmin {
		return nil, numan_error.NewForbidden(numan_error.CodeForbidden, "Seul un Administrateur peut modifier l'interrupteur de délégation.", nil)
	}
	variables.ModerationAdminDelegationEnabled = enabled
	nowMs := getLifecycleTime().UnixMilli()

	numan_log.Info(ctx).
		Bool("delegation_enabled", enabled).
		Msg("Interrupteur de délégation administrative de modération mis à jour.")

	return &moderation_models.AdminDelegationSwitchOutput{
		Enabled:   enabled,
		UpdatedAt: nowMs,
	}, nil
}

// GetDelegationSwitchService lit l'état actuel de l'interrupteur de délégation.
func GetDelegationSwitchService(_ context.Context) bool {
	return variables.ModerationAdminDelegationEnabled
}

// CloseTicketService clôture un ticket, enregistre l'archivage en base froide et envoie le feedback réputationnel aux signaleurs.
func CloseTicketService(ctx context.Context, _ int64, isDismiss bool, rationale string, reportID int64) (*moderation_models.CloseTicketOutput, error) {
	ticket, err := cache_service.GetModerationTicket(ctx, reportID)
	if err != nil {
		return nil, err
	}
	nowMs := getLifecycleTime().UnixMilli()

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
	nowMs := getLifecycleTime().UnixMilli()
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

func clampTrustUnit(v float64) float64 {
	if v < variables.TrustScoreMin {
		return variables.TrustScoreMin
	}
	if v > variables.TrustScoreMax {
		return variables.TrustScoreMax
	}
	return v
}
