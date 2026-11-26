package worker

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleSocialFanOut_AuthorDailyThrottleRetentionCapAndTTL(t *testing.T) {
	mr := test.SetupTestRedis(t)
	ctx := context.Background()

	origMaxPerDay := variables.FanOutMaxPostsPerAuthorPerDay
	origRetention := variables.FanOutRetentionLimit
	variables.FanOutMaxPostsPerAuthorPerDay = 3
	variables.FanOutRetentionLimit = -3 // Conserve uniquement les 2 derniers posts par Mailbox
	defer func() {
		variables.FanOutMaxPostsPerAuthorPerDay = origMaxPerDay
		variables.FanOutRetentionLimit = origRetention
	}()

	authorID := int64(42)
	follower1 := int64(101)
	follower2 := int64(102)

	// Enregistre 2 abonnés dans le Speed Cache (in:1:{authorID})
	inKey := fmt.Sprintf("in:%d:%d", variables.RelationStateFollow, authorID)
	require.NoError(t, redis.SpeedRelationsIndex.ZAdd(ctx, inKey, 1, follower1))
	require.NoError(t, redis.SpeedRelationsIndex.ZAdd(ctx, inKey, 2, follower2))

	// Simule un auteur qui publie 6 posts dans la même journée
	for i := 1; i <= 6; i++ {
		postID := int64(1000 + i)
		require.NoError(t, object_cache_service.SetPostInObjectCache(ctx, post_models.PostPayload{
			ID:         postID,
			UserID:     authorID,
			Visibility: variables.PostVisibilityAll,
		}))
		handleSocialFanOut(ctx, []redis.AsyncEvent{{
			ID:     postID,
			Type:   redis.EntityPost,
			Action: redis.ActionCreate,
		}})
	}

	// 1. Plafond quotidien par auteur : seuls 3 posts ont été autorisés en Fan-Out (posts 1001..1003)
	windowCount, err := redis.FanOutAuthorWindows.ZCard(ctx, authorID)
	require.NoError(t, err)
	assert.Equal(t, int64(3), windowCount, "FanOutAuthorWindows doit plafonner à FanOutMaxPostsPerAuthorPerDay=3")

	// 2. Plafond fixe de taille de la Mailbox (FanOutRetentionLimit=-3 -> 2 posts max) + format "{postID}:{authorID}"
	for _, followerID := range []int64{follower1, follower2} {
		members, errRange := redis.FeedsMailbox.ZRevRange(ctx, followerID, 0, -1)
		require.NoError(t, errRange)
		assert.Equal(t, []string{"1003:42", "1002:42"}, members,
			"la Mailbox du follower %d doit contenir uniquement les 2 derniers posts autorisés au format postID:authorID", followerID)

		// 3. TTL bien appliqué sur FeedsMailbox (StandardTTL = 7 jours)
		ttl, errTTL := redis.FeedsMailbox.TTL(ctx, followerID)
		require.NoError(t, errTTL)
		assert.Greater(t, ttl.Seconds(), float64(0), "FeedsMailbox doit avoir un TTL actif")
		assert.Greater(t, mr.KeyTTL(redis.FeedsMailbox.Key(followerID)).Seconds(), float64(0))
	}

	// Vérifie que la modification à chaud de FanOutMaxPostsPerAuthorPerDay débloque immédiatement le post suivant
	variables.FanOutMaxPostsPerAuthorPerDay = 4
	handleSocialFanOut(ctx, []redis.AsyncEvent{{
		ID:     1004,
		Type:   redis.EntityPost,
		Action: redis.ActionCreate,
	}})

	membersAfterHotReload, err := redis.FeedsMailbox.ZRevRange(ctx, follower1, 0, -1)
	require.NoError(t, err)
	assert.Equal(t, []string{"1004:42", "1003:42"}, membersAfterHotReload)
}
