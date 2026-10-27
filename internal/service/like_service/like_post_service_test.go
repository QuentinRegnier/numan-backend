package like_service_test

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/like_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/like_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTogglePostLike_AdminBonus(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	postAuthorID := int64(888)
	modCallerID := int64(999)
	normalCallerID := int64(111)
	postID := int64(555)

	// Inscription du modérateur dans UsersLite
	require.NoError(t, redis.UsersLite.SetObject(ctx, modCallerID, lite_models.UserLiteRequest{
		ID:         modCallerID,
		Grade:      variables.UserGradeModerator,
		TrustScore: 0.85,
	}))

	// Inscription de l'utilisateur standard dans UsersLite
	require.NoError(t, redis.UsersLite.SetObject(ctx, normalCallerID, lite_models.UserLiteRequest{
		ID:         normalCallerID,
		Grade:      variables.UserGradeUser,
		TrustScore: 0.40,
	}))

	// Enregistrement du post dans le cache objet
	require.NoError(t, object_cache_service.SetPostInObjectCache(ctx, post_models.PostPayload{
		ID:         postID,
		UserID:     postAuthorID,
		Visibility: 0,
	}))

	// 1. Like par utilisateur normal : pas d'événement LikedByAdmin
	errNormal := like_service.TogglePostLike(ctx, normalCallerID, like_models.LikePostInput{
		PostID: postID,
		Action: "like",
	})
	require.NoError(t, errNormal)

	lenQueue, _ := redis.WorkerQueue.LLen(ctx, variables.WorkerQueueTrustScore)
	assert.Equal(t, int64(0), lenQueue, "Un like par un utilisateur normal ne doit pas publier dans la file de réputation")

	// 2. Like par modérateur : événement LikedByAdmin publié dans la file
	errMod := like_service.TogglePostLike(ctx, modCallerID, like_models.LikePostInput{
		PostID: postID,
		Action: "like",
	})
	require.NoError(t, errMod)

	rawEvt, errPop := redis.WorkerQueue.LPop(ctx, variables.WorkerQueueTrustScore)
	require.NoError(t, errPop)
	assert.Contains(t, rawEvt, variables.TrustTriggerLikedByAdmin)
	assert.Contains(t, rawEvt, "888")
}
