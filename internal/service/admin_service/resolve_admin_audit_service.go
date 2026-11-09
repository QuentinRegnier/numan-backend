package admin_service

import (
	"context"
	"fmt"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ResolveAdminAuditService permet à un Administrateur (Grade 4) de trancher l'audit des 3 blâmes.
func ResolveAdminAuditService(ctx context.Context, adminID int64, adminGrade int, input admin_models.ResolveAdminAuditInput) (admin_models.ResolveAdminAuditOutput, error) {
	if adminGrade != variables.UserGradeAdmin {
		return admin_models.ResolveAdminAuditOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Seul un Administrateur (Grade 4) peut statuer sur un audit de blâmes.", nil)
	}

	ticket, err := cache_service.GetModerationTicket(ctx, input.ReportID)
	if err != nil {
		return admin_models.ResolveAdminAuditOutput{}, err
	}
	if ticket.State != variables.ReportStateEscalated || ticket.EscalationType != "blame_quota_audit" {
		return admin_models.ResolveAdminAuditOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Ce ticket n'est pas un audit de quota de 3 blâmes en attente.", nil)
	}

	nowMs := domain.NowMillis()
	_ = cache_service.RemoveTicketFromModerationQueue(ctx, variables.ModerationQueueAdmin, ticket.ReportID)

	out := admin_models.ResolveAdminAuditOutput{}

	if input.Decision == "validate" {
		// DÉCISION FATALE : Confirmation des 3 blâmes -> Bannissement définitif (Hard Ban)
		_, errBan := ExecuteAdminHardBanService(ctx, adminID, admin_models.AdminHardBanInput{
			TargetUserID:    ticket.TargetOwnerID,
			ReportID:        ticket.ReportID,
			Reason:          fmt.Sprintf("Confirmation de l'audit des 3 blâmes : %s", input.Rationale),
			CGUFeedbackNote: "Sanction validée par l'Administration suite à 3 infractions répétées.",
		})
		if errBan != nil {
			return admin_models.ResolveAdminAuditOutput{}, errBan
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
				return admin_models.ResolveAdminAuditOutput{}, errSt
			}
			st.ModeratorTrustScore = clampTrustUnit(st.ModeratorTrustScore + variables.ModTrustValidationBonus)
			if errSave := cache_service.SaveModeratorStats(ctx, st); errSave != nil {
				return admin_models.ResolveAdminAuditOutput{}, errSave
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
			return admin_models.ResolveAdminAuditOutput{}, errStats
		}
		targetStats.BlameHistory = retainedBlames
		targetStats.BlameCount = len(retainedBlames)
		targetStats.UpdatedAt = nowMs
		if errSave := cache_service.SaveModeratorStats(ctx, targetStats); errSave != nil {
			return admin_models.ResolveAdminAuditOutput{}, errSave
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
				return admin_models.ResolveAdminAuditOutput{}, errSt
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
				return admin_models.ResolveAdminAuditOutput{}, errSave
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
		return admin_models.ResolveAdminAuditOutput{}, errSave
	}
	_ = postgres.FuncArchiveClosedReport(ctx, ticket.ReportID, variables.ReportStateClosed, fmt.Sprintf("Résolution Audit Administrateur : %s", ticket.ResolutionOutcome))

	out.Ticket = ToPublicTicketView(ticket, true)
	return out, nil
}
