package admin_service

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAdminAuditService(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		return test.NewMockRows([]string{"archived"}, [][]driver.Value{{true}}), nil
	}, nil)

	targetID := int64(7003)
	modA := int64(3001)
	modB := int64(3002)
	modC := int64(8003)
	adminID := int64(4001)
	superReportID := int64(9999)

	// Initialise les modérateurs au score officiel par défaut (0.85)
	require.NoError(t, cache_service.SaveModeratorStats(ctx, &moderation_models.ModeratorStatsRecord{
		UserID:              modA,
		Grade:               variables.UserGradeModerator,
		ModeratorTrustScore: variables.ModTrustInitial, // 0.85
	}))
	require.NoError(t, cache_service.SaveModeratorStats(ctx, &moderation_models.ModeratorStatsRecord{
		UserID:              modB,
		Grade:               variables.UserGradeModerator,
		ModeratorTrustScore: variables.ModTrustInitial, // 0.85
	}))
	require.NoError(t, cache_service.SaveModeratorStats(ctx, &moderation_models.ModeratorStatsRecord{
		UserID:              modC,
		Grade:               variables.UserGradeModerator,
		ModeratorTrustScore: variables.ModTrustInitial, // 0.85
	}))

	// L'auteur a déjà 3 blâmes
	targetStats := &moderation_models.ModeratorStatsRecord{
		UserID:     targetID,
		BlameCount: 3,
		BlameHistory: []moderation_models.BlameRecord{
			{ReportID: 1, ProposerModID: modA, ValidatorModID: modB, Motivation: "Blame 1"},
			{ReportID: 2, ProposerModID: modA, ValidatorModID: modB, Motivation: "Blame 2"},
			{ReportID: 3, ProposerModID: modA, ValidatorModID: modC, Motivation: "Blame 3"},
		},
	}
	require.NoError(t, cache_service.SaveModeratorStats(ctx, targetStats))

	// Super-Report d'audit simulé
	superReport := &moderation_models.ModerationTicketRecord{
		ReportID:          superReportID,
		TargetType:        variables.ReportTargetUser,
		TargetID:          targetID,
		TargetOwnerID:     targetID,
		Category:          variables.ReportCatOther,
		ReportOrigin:      variables.ReportOriginSystem,
		State:             variables.ReportStateEscalated,
		Reason:            "Quota de 3 blâmes consécutifs atteint (Audit Administrateur requis)",
		StaticBaseScore:   100.0,
		PriorityScore:     100.0,
		EscalationType:    "blame_quota_audit",
		AuditBlameRecords: targetStats.BlameHistory,
	}
	require.NoError(t, cache_service.SaveModerationTicket(ctx, superReport))
	require.NoError(t, cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueueAdmin, superReportID, 100.0))

	// 1. Révocation PARTIELLE : l'admin ne révoque que le blâme 1 (sur les 3)
	revokedReportID := superReport.AuditBlameRecords[0].ReportID
	partialAuditRes, errPartialAudit := ResolveAdminAuditService(ctx, adminID, variables.UserGradeAdmin, admin_models.ResolveAdminAuditInput{
		ReportID:              superReportID,
		Decision:              "revoke",
		Rationale:             "Le premier blâme était infondé, les deux autres restent à l'étude.",
		RevokedBlameReportIDs: []int64{revokedReportID},
	})

	require.NoError(t, errPartialAudit)
	assert.False(t, partialAuditRes.TargetBanned)
	assert.Contains(t, partialAuditRes.PenalizedModIDs, modA)
	assert.Contains(t, partialAuditRes.PenalizedModIDs, modB)
	assert.NotContains(t, partialAuditRes.PenalizedModIDs, modC, "ModC n'a pas instruit le blâme 1 et ne doit pas être pénalisé")
	assert.Empty(t, partialAuditRes.DemotedModIDs, "Avec 0.85 au départ, une seule pénalité de -0.25 (-> 0.60) ne rétrograde pas (seuil = 0.50)")

	// Vérifie que ModA est passé de 0.85 à 0.60
	modAStats1, _ := cache_service.GetOrInitModeratorStats(ctx, modA, 0)
	assert.InDelta(t, 0.60, modAStats1.ModeratorTrustScore, 1e-4)
	assert.Equal(t, variables.UserGradeModerator, modAStats1.Grade)

	// Vérifie que le compteur de blâmes de la cible est à 2 (3 - 1 révoqué)
	targetStatsPartial, _ := cache_service.GetOrInitModeratorStats(ctx, targetID, 0)
	assert.Equal(t, 2, targetStatsPartial.BlameCount)

	// Vérifie la publication de l'événement de réputation TrustTriggerSuccessfulAppeal (+0.08)
	rawAppeal, errAppeal := redis.WorkerQueue.LPop(ctx, variables.WorkerQueueTrustScore)
	require.NoError(t, errAppeal)
	assert.Contains(t, rawAppeal, variables.TrustTriggerSuccessfulAppeal)

	// 2. Révocation TOTALE : réouverture pour audit des blâmes restants et révocation complète
	superReport.State = variables.ReportStateEscalated
	require.NoError(t, cache_service.SaveModerationTicket(ctx, superReport))

	fullAuditRes, errFullAudit := ResolveAdminAuditService(ctx, adminID, variables.UserGradeAdmin, admin_models.ResolveAdminAuditInput{
		ReportID:  superReportID,
		Decision:  "revoke",
		Rationale: "Après réexamen approfondi, tous les blâmes sont annulés.",
	})
	require.NoError(t, errFullAudit)
	assert.Contains(t, fullAuditRes.PenalizedModIDs, modA)
	assert.Contains(t, fullAuditRes.PenalizedModIDs, modC)

	// ModA subit une 2ème pénalité : 0.60 - 0.25 = 0.35 (< 0.50) -> Rétrogradation automatique au Grade User (0) !
	assert.Contains(t, fullAuditRes.DemotedModIDs, modA)
	modAStats2, _ := cache_service.GetOrInitModeratorStats(ctx, modA, 0)
	assert.Equal(t, variables.UserGradeUser, modAStats2.Grade)
	assert.InDelta(t, 0.35, modAStats2.ModeratorTrustScore, 1e-4)

	// Vérifie que le compteur de la cible est désormais totalement réinitialisé à 0
	restoredTargetStats, _ := cache_service.GetOrInitModeratorStats(ctx, targetID, 0)
	assert.Equal(t, 0, restoredTargetStats.BlameCount)
}
