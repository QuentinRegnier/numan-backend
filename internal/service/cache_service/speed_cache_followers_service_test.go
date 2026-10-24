package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestSpeedCacheFollowersService(t *testing.T) {
	ctx := context.Background()

	t.Run("RelationValue L1 Hit, L2 Hit, L3 Hit, and L3 Error", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
		restore := test.ResetCollections()
		defer restore()

		// 1. L1 Hit
		require.NoError(t, redis.SpeedRelations.HSet(ctx, 20, "10", variables.RelationStateFriend))
		assert.Equal(t, variables.RelationStateFriend, RelationValue(ctx, 20, 10))

		// 2. L1 Miss -> L2 Mongo Hit
		mt.Run("L2 Mongo Hit", func(mt *mtest.T) {
			repoMongo.Relations = test.NewMockCollection(mt, "auth.relations", nil)
			mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.relations", mtest.FirstBatch, bson.D{
				{Key: "caller_id", Value: int64(10)},
				{Key: "target_id", Value: int64(30)},
				{Key: "state", Value: int32(variables.RelationStateFollow)},
			}))

			srv.SetFailCmd("HSET", errors.New("hset warn"))
			assert.Equal(t, variables.RelationStateFollow, RelationValue(ctx, 30, 10))
			srv.SetFailCmd("HSET", nil)
		})

		// 3. L1 & L2 Miss -> L3 Postgres Hit + Async EnqueueDB
		mt.Run("L3 Postgres Hit", func(mt *mtest.T) {
			repoMongo.Relations = test.NewMockCollection(mt, "auth.relations", nil)
			mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.relations", mtest.FirstBatch))

			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return test.NewMockRows([]string{"func_get_relation_state"}, [][]driver.Value{{int64(variables.RelationStateBlocked)}}), nil
			}, nil)

			assert.Equal(t, variables.RelationStateBlocked, RelationValue(ctx, 40, 10))
			time.Sleep(20 * time.Millisecond)
		})

		// 4. L3 Postgres Error -> RelationStateNone
		mt.Run("L3 Postgres Error", func(mt *mtest.T) {
			repoMongo.Relations = test.NewMockCollection(mt, "auth.relations", nil)
			mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.relations", mtest.FirstBatch))

			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return nil, errors.New("pg down")
			}, nil)

			assert.Equal(t, variables.RelationStateNone, RelationValue(ctx, 50, 10))
		})
	})

	t.Run("UpdateRelationState and Index Queries", func(t *testing.T) {
		srv := test.SetupTestRedis(t)

		// Follow
		require.NoError(t, UpdateRelationState(ctx, 100, 1, variables.RelationStateFollow, 1000))
		require.NoError(t, UpdateRelationState(ctx, 100, 2, variables.RelationStateFollow, 2000))

		followers, err := GetSpeedRelationsIndex(ctx, 100)
		require.NoError(t, err)
		assert.Equal(t, []int64{2, 1}, followers)
		assert.Equal(t, int64(2), GetFollowerCount(ctx, 100))

		// Promote user 1 to Friend
		require.NoError(t, UpdateRelationState(ctx, 100, 1, variables.RelationStateFriend, 3000))
		friends, err := GetSpeedFriends(ctx, 100)
		require.NoError(t, err)
		assert.Equal(t, []int64{1}, friends)

		// Block user 200 from caller 1
		require.NoError(t, UpdateRelationState(ctx, 200, 1, variables.RelationStateBlocked, 4000))
		blocked, err := GetRelationsByDirectionPaginatedFromCache(ctx, 1, variables.RelationStateBlocked, "outgoing", 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []int64{200}, blocked)

		incomingFriends, err := GetRelationsByDirectionPaginatedFromCache(ctx, 100, variables.RelationStateFriend, "incoming", 10, 0)
		require.NoError(t, err)
		assert.Equal(t, []int64{1}, incomingFriends)

		// Error paths
		var appErr *numan_error.AppError
		srv.SetFailCmd("HSET", errors.New("hset err"))
		require.ErrorAs(t, UpdateRelationState(ctx, 100, 1, variables.RelationStateFollow, 1000), &appErr)
		srv.SetFailCmd("HSET", nil)

		srv.SetFailCmd("ZREM", errors.New("zrem warn"))
		srv.SetFailCmd("ZADD", errors.New("zadd err"))
		require.ErrorAs(t, UpdateRelationState(ctx, 100, 1, variables.RelationStateFollow, 1000), &appErr)
		require.ErrorAs(t, UpdateRelationState(ctx, 100, 1, variables.RelationStateFriend, 1000), &appErr)
		require.ErrorAs(t, UpdateRelationState(ctx, 100, 1, variables.RelationStateBlocked, 1000), &appErr)
		srv.SetFailCmd("ZREM", nil)
		srv.SetFailCmd("ZADD", nil)

		srv.SetFailCmd("ZCARD", errors.New("zcard err"))
		assert.Equal(t, int64(0), GetFollowerCount(ctx, 100))
		srv.SetFailCmd("ZCARD", nil)

		srv.SetFailCmd("ZREVRANGE", errors.New("zrev err"))
		_, err = GetSpeedRelationsIndex(ctx, 100)
		require.ErrorAs(t, err, &appErr)
		_, err = GetSpeedFriends(ctx, 100)
		require.ErrorAs(t, err, &appErr)
		_, err = GetRelationsByDirectionPaginatedFromCache(ctx, 100, variables.RelationStateFollow, "incoming", 10, 0)
		require.ErrorAs(t, err, &appErr)
	})
}
