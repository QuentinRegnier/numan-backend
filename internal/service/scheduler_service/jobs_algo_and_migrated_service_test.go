package scheduler_service_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlgoAndCacheJobsExecution(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	eng := scheduler_service.NewEngine("dc-eu-1", scheduler_service.DefaultRegimeConfig(), nil)
	scheduler_service.RegisterAllDefaultJobs(eng)

	// 1. most_cache_surgical_purify : supprime le post masqué (-1), garde le post sain (+1)
	require.NoError(t, repoRedis.Posts.SetObject(ctx, 501, post_models.PostPayload{ID: 501, Visibility: 1}))
	require.NoError(t, repoRedis.Posts.SetObject(ctx, 502, post_models.PostPayload{ID: 502, Visibility: -1}))
	subKey := variables.StrictRankSubKey(variables.StrictTypeRecent, variables.DefaultContentLanguage)
	rankKey := repoRedis.RankedPosts.Key(subKey)
	require.NoError(t, repoRedis.ZAddOnCore(ctx, rankKey, 1234.5, "501"))
	require.NoError(t, repoRedis.ZAddOnCore(ctx, rankKey, 900.0, "502"))

	st, err := eng.TriggerJob(ctx, "most_cache_surgical_purify", true)
	require.NoError(t, err)
	assert.Equal(t, "completed", st.Status)

	members, err := repoRedis.ZRangeOnCore(ctx, rankKey, 0, -1)
	require.NoError(t, err)
	assert.Equal(t, []string{"501"}, members)

	// 2. social_graph_2hop_3hop_suggestions : utilisateur 10 ami avec 20 & 30 qui sont tous deux amis avec 40 -> 40 ajouté dans SpeedAddable(10)
	require.NoError(t, repoRedis.SpeedRelations.SAdd(ctx, 10, "20", "30"))
	require.NoError(t, repoRedis.SpeedRelations.SAdd(ctx, 20, "10", "40"))
	require.NoError(t, repoRedis.SpeedRelations.SAdd(ctx, 30, "10", "40"))

	st2, err := eng.TriggerJob(ctx, "social_graph_2hop_3hop_suggestions", true)
	require.NoError(t, err)
	assert.Equal(t, "completed", st2.Status)

	addable, err := repoRedis.SpeedAddable.SMembers(ctx, 10)
	require.NoError(t, err)
	assert.Contains(t, addable, "40")
}
