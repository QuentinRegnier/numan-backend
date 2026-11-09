package admin_service

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelpers_ClampTrustUnit(t *testing.T) {
	assert.Equal(t, variables.TrustScoreMin, clampTrustUnit(-0.5))
	assert.Equal(t, variables.TrustScoreMax, clampTrustUnit(1.5))
	assert.Equal(t, 0.5, clampTrustUnit(0.5))
}

func TestHelpers_PublishTrustScoreEvent(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	err := PublishTrustScoreEvent(ctx, 123, variables.TrustTriggerMajorInfraction, 456, "Test reason")
	require.NoError(t, err)

	// Vérification de la présence dans la file Redis
	raw, errPop := redis.WorkerQueue.LPop(ctx, variables.WorkerQueueTrustScore)
	require.NoError(t, errPop)
	assert.Contains(t, raw, variables.TrustTriggerMajorInfraction)
	assert.Contains(t, raw, "Test reason")
}

func TestHelpers_ToPublicTicketView(t *testing.T) {
	now := domain.NowMillis()
	record := &moderation_models.ModerationTicketRecord{
		ReportID:          12345,
		State:             3, // PendingPeerReview
		ClaimedByModID:    4001,
		ProposerPseudonym: "mod_anon_test",
		AuditBlameRecords: []moderation_models.BlameRecord{
			{ReportID: 100, ProposerModID: 4001, ValidatorModID: 4002, Motivation: "Blame 1", CreatedAt: now},
		},
	}

	// 1. Vue pour Admin (includeAdminAuditDetails = true)
	viewAdmin := ToPublicTicketView(record, true)
	assert.Equal(t, int64(0), viewAdmin.ClaimedByModID, "Masqué même pour l'admin lors du state 3 par conception (règle des 4 yeux)")
	assert.Equal(t, "mod_anon_test", viewAdmin.ProposerPseudonym)
	require.Len(t, viewAdmin.AuditBlameRecords, 1)

	// 2. Vue non-admin (includeAdminAuditDetails = false)
	viewUser := ToPublicTicketView(record, false)
	assert.Equal(t, int64(0), viewUser.ClaimedByModID)
	assert.Empty(t, viewUser.ProposerPseudonym)
	assert.Empty(t, viewUser.AuditBlameRecords)
}
