package worker

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClampTrustScore_Bounds(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{name: "negative clamped to 0.00", input: -0.15, expected: 0.00},
		{name: "above 1 clamped to 1.00", input: 1.25, expected: 1.00},
		{name: "initial score preserved", input: 0.20, expected: 0.20},
		{name: "precision rounded cleanly", input: 0.20 + 0.05 + 0.02, expected: 0.27},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, ClampTrustScore(tc.input))
		})
	}
}

func TestComputeTrustScoreDelta_AllEvents(t *testing.T) {
	delta, isInf, isMajor, ok := ComputeTrustScoreDelta(variables.TrustEventQuarantineApproved)
	require.True(t, ok)
	assert.Equal(t, 0.05, delta)
	assert.False(t, isInf)
	assert.False(t, isMajor)

	delta, isInf, isMajor, ok = ComputeTrustScoreDelta(variables.TrustEventMinorInfraction)
	require.True(t, ok)
	assert.Equal(t, variables.TrustMalusMinorInfraction, delta)
	assert.True(t, isInf)
	assert.False(t, isMajor)

	_, isInf, isMajor, ok = ComputeTrustScoreDelta(variables.TrustEventMajorInfraction)
	require.True(t, ok)
	assert.True(t, isInf)
	assert.True(t, isMajor)

	_, _, _, ok = ComputeTrustScoreDelta("UNKNOWN_EVENT")
	assert.False(t, ok)
}

func TestProcessTrustScoreBatch_UpdatesL1AndL3_AndHandlesMajorBan(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	// Pré-hydratation d'un utilisateur dans le Speed Cache L1
	user1 := lite_models.UserLiteRequest{ID: 101, Username: "alice", TrustScore: 0.38}
	user2 := lite_models.UserLiteRequest{ID: 202, Username: "mallory", TrustScore: 0.50}
	require.NoError(t, redis.UsersLite.SetObject(ctx, user1.ID, user1))
	require.NoError(t, redis.UsersLite.SetObject(ctx, user2.ID, user2))

	// Simulation PostgreSQL L3 via SetupMockDB centralisé
	_, _ = test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		if strings.Contains(query, "func_apply_trust_score_delta") {
			uid := args[0].Value.(int64)
			if uid == 101 {
				// 0.38 + 0.05 = 0.43 (sort de quarantaine !)
				return test.NewMockRows([]string{"new_score", "is_banned"}, [][]driver.Value{{0.43, false}}), nil
			}
			if uid == 202 {
				// Major infraction -> 0.00 + banned
				return test.NewMockRows([]string{"new_score", "is_banned"}, [][]driver.Value{{0.00, true}}), nil
			}
		}
		if strings.Contains(query, "func_load_sessions") {
			return test.NewMockRows(
				[]string{"id", "user_id", "master_token", "firebase_installation_id", "device_info", "ip_history", "created_at", "expires_at"},
				[][]driver.Value{},
			), nil
		}
		return test.NewMockRows([]string{}, nil), nil
	}, nil)

	// Enqueue d'un bonus de quarantaine (+0.05) pour Alice et d'une infraction majeure pour Mallory
	require.NoError(t, EnqueueTrustScoreEvent(ctx, moderation_models.TrustScoreEventPayload{
		EventID:   1,
		UserID:    101,
		EventType: variables.TrustEventQuarantineApproved,
	}))
	require.NoError(t, EnqueueTrustScoreEvent(ctx, moderation_models.TrustScoreEventPayload{
		EventID:   2,
		UserID:    202,
		EventType: variables.TrustEventMajorInfraction,
		Reason:    "Contenu non-consenti",
		TargetID:  999,
	}))

	// Exécution du batch
	drainAndProcessTrustEvents(ctx)

	// Vérification L1 pour Alice (passée à 0.43 -> Zone Standard)
	var updatedAlice lite_models.UserLiteRequest
	require.NoError(t, redis.UsersLite.GetObject(ctx, 101, &updatedAlice))
	assert.Equal(t, 0.43, updatedAlice.TrustScore)

	// Vérification L1 et Ban Stéganographique pour Mallory (tombée à 0.00)
	var updatedMallory lite_models.UserLiteRequest
	require.NoError(t, redis.UsersLite.GetObject(ctx, 202, &updatedMallory))
	assert.Equal(t, 0.00, updatedMallory.TrustScore)
	assert.True(t, IsSteganoBanned(ctx, 202))
}

func TestProcessTrustScoreBatch_ModBatchLikeExpansion(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	u1 := lite_models.UserLiteRequest{ID: 301, Username: "bob", TrustScore: 0.50}
	u2 := lite_models.UserLiteRequest{ID: 302, Username: "charlie", TrustScore: 0.60}
	require.NoError(t, redis.UsersLite.SetObject(ctx, u1.ID, u1))
	require.NoError(t, redis.UsersLite.SetObject(ctx, u2.ID, u2))

	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		if strings.Contains(query, "func_apply_trust_score_delta") {
			uid := args[0].Value.(int64)
			if uid == 301 {
				return test.NewMockRows([]string{"new_score", "is_banned"}, [][]driver.Value{{0.505, false}}), nil
			}
			if uid == 302 {
				return test.NewMockRows([]string{"new_score", "is_banned"}, [][]driver.Value{{0.6032, false}}), nil
			}
		}
		return test.NewMockRows([]string{}, nil), nil
	}, nil)

	// Publication d'un événement groupé mod_batch_like avec UserIDs
	require.NoError(t, EnqueueTrustScoreEvent(ctx, moderation_models.TrustScoreEventPayload{
		EventID:   10,
		UserIDs:   []int64{301, 302},
		EventType: variables.TrustTriggerModBatchLike,
		Reason:    "Validation groupée de publications qualitatives",
	}))

	drainAndProcessTrustEvents(ctx)

	var resBob lite_models.UserLiteRequest
	require.NoError(t, redis.UsersLite.GetObject(ctx, 301, &resBob))
	assert.Greater(t, resBob.TrustScore, 0.50)

	var resCharlie lite_models.UserLiteRequest
	require.NoError(t, redis.UsersLite.GetObject(ctx, 302, &resCharlie))
	assert.Greater(t, resCharlie.TrustScore, 0.60)
}

func TestProcessMonthlyCleanBonus(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		if strings.Contains(query, "SELECT id FROM auth.users") {
			return test.NewMockRows([]string{"id"}, [][]driver.Value{{int64(777)}}), nil
		}
		return test.NewMockRows([]string{}, nil), nil
	}, nil)

	ProcessMonthlyCleanBonus(ctx)

	raw, err := redis.WorkerQueue.LPop(ctx, variables.WorkerQueueTrustScore)
	require.NoError(t, err)
	assert.Contains(t, raw, variables.TrustTriggerMonthlyClean)
}
