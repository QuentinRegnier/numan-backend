package user_recommendation_service_test

import (
	"context"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/user_recommendation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedTestUserForReco(
	t *testing.T,
	ctx context.Context,
	id int64,
	username string,
	lang int,
	location string,
	showLocation bool,
	banned bool,
	desactivated bool,
	vecVal float32,
) {
	t.Helper()

	uPayload := auth_models.UserPayload{
		ID:           id,
		Username:     username,
		FirstName:    username,
		LastName:     "Test",
		Location:     location,
		Banned:       banned,
		Desactivated: desactivated,
		TrustScore:   0.8,
	}
	require.NoError(t, redis.Users.SetObject(ctx, id, uPayload))

	var vec []float32
	if vecVal != 0 {
		vec = make([]float32, variables.VectorDimTotal)
		for i := range vec {
			vec[i] = vecVal
		}
	}

	sPayload := user_settings_models.UserSettingsPayload{
		ID:     id,
		UserID: id,
		Privacy: user_settings_models.PrivacySettings{
			ShowLocation:     showLocation,
			ShowOnlineStatus: true,
		},
		DisplayAndContent: user_settings_models.DisplayAndContentSettings{
			Language: lang,
		},
		TelemetryVector: vec,
	}
	require.NoError(t, object_cache_service.SetUserSettings(ctx, sPayload))
	require.NoError(t, cache_service.AddUserToSpeedCache(ctx, uPayload, sPayload))

	if len(vec) == variables.VectorDimTotal {
		require.NoError(t, cache_service.SetTelemetryProfile(ctx, id, lite_models.TelemetryProfileLite{
			Vector:          vec,
			ConfidenceScore: 0.8,
			TimestampMs:     time.Now().UnixMilli(),
		}))
	}
}

func TestGenerateAndGetUserRecommendations_FullPipeline(t *testing.T) {
	_ = test.SetupTestRedis(t)
	defer test.ResetCollections()()
	mongo.UserRecoSkips = nil
	ctx := context.Background()

	targetID := int64(100)
	// Target user in Paris, language 0, vector filled with 1.0
	seedTestUserForReco(t, ctx, targetID, "target_user", 0, "48.8566,2.3522", true, false, false, 1.0)

	// Candidate 101: Very close in Paris ("48.8570,2.3530"), lang 0, low vector (0.1), 0 mutual friends
	seedTestUserForReco(t, ctx, 101, "cand_geo_top", 0, "48.8570,2.3530", true, false, false, 0.1)

	// Candidate 102: Far away ("tokyo"), lang 0, 3 mutual friends (friends 501, 502, 503), shared community 9001
	seedTestUserForReco(t, ctx, 102, "cand_aff_top", 0, "tokyo", true, false, false, 0.1)

	// Candidate 103: Location hidden, lang 0, high vector similarity (2.0)
	seedTestUserForReco(t, ctx, 103, "cand_taste_top", 0, "Paris", false, false, false, 2.0)

	// Excluded candidates:
	// - 104: Already a direct friend of targetID
	seedTestUserForReco(t, ctx, 104, "existing_friend", 0, "Paris", true, false, false, 1.0)
	require.NoError(t, cache_service.UpdateRelationState(ctx, targetID, 104, variables.RelationStateFriend, 1000))

	// - 105: Already skipped by targetID
	seedTestUserForReco(t, ctx, 105, "skipped_cand", 0, "Paris", true, false, false, 1.0)
	require.NoError(t, cache_service.AddUserRecoSkipToCuckoo(ctx, targetID, 105))

	// - 106: Banned user
	seedTestUserForReco(t, ctx, 106, "banned_cand", 0, "Paris", true, true, false, 1.0)
	_ = cache_service.AddUserToLanguageIndex(ctx, 106, 0)

	// - 107: Different language (lang 1)
	seedTestUserForReco(t, ctx, 107, "other_lang_cand", 1, "Paris", true, false, false, 1.0)

	// Setup 3 mutual friends (501, 502, 503) between targetID (100) and candidate 102
	for _, mfID := range []int64{501, 502, 503} {
		require.NoError(t, cache_service.UpdateRelationState(ctx, targetID, mfID, variables.RelationStateFriend, 1000))
		require.NoError(t, cache_service.UpdateRelationState(ctx, 102, mfID, variables.RelationStateFriend, 1000))
	}

	// Setup shared community 9001 between targetID (100) and candidate 102
	require.NoError(t, cache_service.StoreCommunityLiteInSpeedCache(ctx, lite_models.CommunityLiteRequest{
		ID:          9001,
		Name:        "golang-builders",
		MemberCount: 5,
	}))
	require.NoError(t, cache_service.AddConversationToUserInbox(ctx, targetID, 9001, 1))
	require.NoError(t, redis.ConvParticipants.SAdd(ctx, 9001, targetID, 102))

	// Execute asynchronous recommendation engine
	err := user_recommendation_service.GenerateUserRecommendations(ctx, targetID)
	require.NoError(t, err)

	// 1. Verify Geo Ranking: 101 (Paris) > 102 (Tokyo), while 103 has ShowLocation=false (excluded from geo list)
	geoOut, err := user_recommendation_service.GetUserRecommendations(ctx, targetID, user_recommendation_models.GetUserRecommendationsInput{
		Sort:  variables.UserRecoSortGeo,
		Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, geoOut.Users, 2)
	assert.Equal(t, int64(101), geoOut.Users[0].User.ID)
	assert.Equal(t, int64(102), geoOut.Users[1].User.ID)

	// 2. Verify Affinity Ranking: 102 (3 mutual friends + shared community) is #1
	affOut, err := user_recommendation_service.GetUserRecommendations(ctx, targetID, user_recommendation_models.GetUserRecommendationsInput{
		Sort:  variables.UserRecoSortAff,
		Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, affOut.Users, 3)
	assert.Equal(t, int64(102), affOut.Users[0].User.ID)

	// 3. Verify Taste Ranking: 103 (vector 2.0) is #1
	tasteOut, err := user_recommendation_service.GetUserRecommendations(ctx, targetID, user_recommendation_models.GetUserRecommendationsInput{
		Sort:  variables.UserRecoSortTaste,
		Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, tasteOut.Users, 3)
	assert.Equal(t, int64(103), tasteOut.Users[0].User.ID)

	// 4. Verify Global Interleaving (Round-Robin: Geo#1=101 -> Aff#1=102 -> Taste#1=103)
	globalOut, err := user_recommendation_service.GetUserRecommendations(ctx, targetID, user_recommendation_models.GetUserRecommendationsInput{
		Sort:  variables.UserRecoSortGlobal,
		Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, globalOut.Users, 3)
	assert.Equal(t, int64(101), globalOut.Users[0].User.ID)
	assert.Equal(t, int64(102), globalOut.Users[1].User.ID)
	assert.Equal(t, int64(103), globalOut.Users[2].User.ID)
}

func TestSkipUserRecommendation_AndForceRefreshLock(t *testing.T) {
	_ = test.SetupTestRedis(t)
	defer test.ResetCollections()()
	mongo.UserRecoSkips = nil
	ctx := context.Background()

	callerID := int64(300)
	seedTestUserForReco(t, ctx, callerID, "caller_300", 0, "Paris", true, false, false, 1.0)
	seedTestUserForReco(t, ctx, 301, "cand_301", 0, "Paris", true, false, false, 1.0)
	seedTestUserForReco(t, ctx, 302, "cand_302", 0, "Paris", true, false, false, 0.8)

	require.NoError(t, user_recommendation_service.GenerateUserRecommendations(ctx, callerID))

	// Self-skip must be rejected
	_, errSelf := user_recommendation_service.SkipUserRecommendation(ctx, callerID, user_recommendation_models.SkipUserRecommendationInput{
		SkippedUserID: callerID,
	})
	assert.Error(t, errSelf)

	// Valid skip of 301 removes 301 immediately from L1 recommendations and enqueues L2 persistence
	skipOut, errSkip := user_recommendation_service.SkipUserRecommendation(ctx, callerID, user_recommendation_models.SkipUserRecommendationInput{
		SkippedUserID: 301,
	})
	require.NoError(t, errSkip)
	assert.Equal(t, "ok", skipOut.Status)
	assert.Equal(t, int64(301), skipOut.SkippedUserID)

	recosAfterSkip, errGet := user_recommendation_service.GetUserRecommendations(ctx, callerID, user_recommendation_models.GetUserRecommendationsInput{
		Sort:  variables.UserRecoSortGlobal,
		Limit: 10,
	})
	require.NoError(t, errGet)
	require.Len(t, recosAfterSkip.Users, 1)
	assert.Equal(t, int64(302), recosAfterSkip.Users[0].User.ID)

	// Force=true acquires the 12-hour compute lock and enqueues ActionGenerateUserRecommendations
	_, errForce := user_recommendation_service.GetUserRecommendations(ctx, callerID, user_recommendation_models.GetUserRecommendationsInput{
		Sort:  variables.UserRecoSortGlobal,
		Limit: 10,
		Force: true,
	})
	require.NoError(t, errForce)

	hasLock, errLock := cache_service.HasUserRecoComputeLock(ctx, callerID)
	require.NoError(t, errLock)
	assert.True(t, hasLock)
}

func TestGetUserRecommendations_CacheMissBoundedWait(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	origWait := variables.UserRecoSyncWaitTimeout
	variables.UserRecoSyncWaitTimeout = 20 * time.Millisecond
	defer func() {
		variables.UserRecoSyncWaitTimeout = origWait
	}()

	// User 888 has no cached recommendations -> enqueues job, waits up to 20ms, returns empty non-nil slice
	out, err := user_recommendation_service.GetUserRecommendations(ctx, 888, user_recommendation_models.GetUserRecommendationsInput{
		Sort:  variables.UserRecoSortGlobal,
		Limit: 5,
	})
	require.NoError(t, err)
	assert.NotNil(t, out.Users)
	assert.Empty(t, out.Users)

	// Verify job was enqueued in caller's shard
	shardID := redis.GetShardID(888)
	events, errPop := redis.PopSmartBatchBlocking(ctx, shardID, 10)
	require.NoError(t, errPop)
	require.NotEmpty(t, events)
	assert.Equal(t, redis.ActionGenerateUserRecommendations, events[0].Action)
}
