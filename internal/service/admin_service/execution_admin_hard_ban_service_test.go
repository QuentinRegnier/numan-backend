package admin_service

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteAdminHardBanService(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	adminID := int64(4001)
	targetID := int64(7004)

	// Prépare un profil UserLite actif dans le cache SPEED L1
	uLite := lite_models.UserLiteRequest{
		ID:         targetID,
		Username:   "target_violator",
		TrustScore: 0.75,
		Grade:      0,
	}
	require.NoError(t, redis.UsersLite.SetObject(ctx, targetID, uLite))

	// Hard Ban exécuté par l'Administration
	banRes, errBan := ExecuteAdminHardBanService(ctx, adminID, admin_models.AdminHardBanInput{
		TargetUserID:    targetID,
		Reason:          "Pédopornographie avérée (CSAM)",
		CGUFeedbackNote: "Infraction d'une gravité absolue. Signalement PHAROS effectué.",
	})
	require.NoError(t, errBan)
	assert.True(t, banRes.Banned)
	assert.Equal(t, variables.TrustScoreHardBan, banRes.TrustScore)

	// Assertions strictes du Hard Ban :
	// 1. T = 0.00 et compte mis à jour dans le cache SPEED L1
	bannedUserLite, errBannedLite := cache_service.GetUserLite(ctx, targetID)
	require.NoError(t, errBannedLite)
	assert.Equal(t, variables.TrustScoreHardBan, bannedUserLite.TrustScore, "Le Trust Score doit être réinitialisé à 0.00")

	// 2. Bannissement stéganographique enregistré dans le Set Redis O(1)
	isStegano, errStegano := redis.SteganoBanlist.SIsMember(ctx, "global", fmt.Sprintf("%d", targetID))
	require.NoError(t, errStegano)
	assert.True(t, isStegano, "L'identifiant stéganographique doit être inscrit dans le Set Redis")
}
