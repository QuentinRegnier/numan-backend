package admin_service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ExecuteAdminHardBanService exécute le bannissement définitif d'un compte (Hard Ban immédiat).
func ExecuteAdminHardBanService(ctx context.Context, adminID int64, input admin_models.AdminHardBanInput) (admin_models.AdminHardBanOutput, error) {
	nowMs := domain.NowMillis()

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

	return admin_models.AdminHardBanOutput{
		TargetUserID:    input.TargetUserID,
		Banned:          true,
		TrustScore:      variables.TrustScoreHardBan,
		CGUFeedbackNote: input.CGUFeedbackNote,
		ExecutedAt:      nowMs,
	}, nil
}
