package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraPg "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestMostCacheAndPostHydrator(t *testing.T) {
	ctx := context.Background()

	t.Run("EvaluatePostAfterLike, EvaluatePostAfterView, and UpdateScoreWithMetrics across priority levels", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		require.NoError(t, redis.Tags.SAdd(ctx, "active", "golang", "redis"))

		for priority := 0; priority <= 4; priority++ {
			post := post_models.PostPayload{
				ID:               int64(100 + priority),
				LikeCount:        10,
				CommentCount:     5,
				ViewCount:        100,
				HasMedia:         true,
				CreatedAt:        time.Now().UnixMilli(),
				Hashtags:         []string{"golang"},
				IndirectHashtags: []string{"redis"},
				Visibility:       1,
				PriorityLevel:    priority,
			}
			EvaluatePostAfterLike(ctx, post)
			EvaluatePostAfterView(ctx, post)
		}

		// Error branches in Evaluate and UpdateScoreWithMetrics / updateTrendZSETs
		srv.SetFailCmd("EVAL", errors.New("eval err"))
		EvaluatePostAfterLike(ctx, post_models.PostPayload{ID: 999, Hashtags: []string{"golang"}})
		EvaluatePostAfterView(ctx, post_models.PostPayload{ID: 999, Hashtags: []string{"golang"}})
		srv.SetFailCmd("EVAL", nil)

		// Direct call to updateTrendZSETs
		require.NoError(t, updateTrendZSETs(ctx, 100, 50.0, []string{"golang"}, []string{"redis"}, "20261024", "2026102418", "2026-W43"))
	})

	t.Run("GetRankedPosts L1 Hit and L2/L3 Fallbacks for strict:likes, strict:views, strict:recent", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
		restore := test.ResetCollections()
		defer restore()

		// 1. L1 Hit via fetchAndHydrateFromCollection
		post1 := post_models.PostPayload{ID: 501, Content: "Top Post", Visibility: 1}
		require.NoError(t, object_cache_service.SetPostInObjectCache(ctx, post1))
		require.NoError(t, redis.RankedPosts.ZAdd(ctx, variables.RedisKeyStrictLikes, 100, "501"))
		require.NoError(t, redis.RankedPosts.ZAdd(ctx, variables.RedisKeyStrictLikes, 50, "invalid-id"))

		posts, err := GetRankedPosts(ctx, variables.RedisKeyStrictLikes, 0, 10)
		require.NoError(t, err)
		require.Len(t, posts, 1)
		assert.Equal(t, int64(501), posts[0].ID)

		// Empty ZSET returns empty slice
		emptyPosts, err := GetRankedPosts(ctx, variables.RedisKeyStrictViews, 0, 10)
		require.NoError(t, err)
		assert.Empty(t, emptyPosts)

		// L1 ZRevRange error
		srv.SetFailCmd("ZREVRANGE", errors.New("zrev err"))
		_, err = GetRankedPosts(ctx, variables.RedisKeyStrictLikes, 0, 10)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("ZREVRANGE", nil)

		// 2. Offset >= MaxStrictElements -> L2 Mongo Hit
		mt.Run("L2 Mongo Hit on deep pagination", func(mt *mtest.T) {
			repoMongo.Posts = test.NewMockCollection(mt, "content.posts", nil)
			mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.content.posts", mtest.FirstBatch, bson.D{
				{Key: "id", Value: int64(601)},
				{Key: "content", Value: "Deep L2 Post"},
				{Key: "visibility", Value: 1},
			}))

			deepOffset := int64(variables.MaxStrictElements)
			res, err := GetRankedPosts(ctx, variables.RedisKeyStrictLikes, deepOffset, 5)
			require.NoError(t, err)
			require.Len(t, res, 1)
			assert.Equal(t, int64(601), res[0].ID)
		})

		// 3. Offset >= MaxStrictElements -> L2 Miss/Error -> L3 Postgres Fallback (verifying strict:likes and strict:views SQL queries)
		for _, rankType := range []string{variables.RedisKeyStrictLikes, variables.RedisKeyStrictViews, variables.RedisKeyStrictRecent, "unknown"} {
			mt.Run("L3 Postgres Fallback for "+rankType, func(mt *mtest.T) {
				repoMongo.Posts = test.NewMockCollection(mt, "content.posts", nil)
				mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 1, Message: "mongo err"}))

				var capturedQuery string
				test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
					capturedQuery = query
					return test.NewMockRows([]string{"id"}, [][]driver.Value{{int64(501)}}), nil
				}, nil)

				deepOffset := int64(variables.MaxStrictElements)
				res, err := GetRankedPosts(ctx, rankType, deepOffset, 5)
				require.NoError(t, err)
				require.Len(t, res, 1)
				assert.Equal(t, int64(501), res[0].ID)

				if rankType == variables.RedisKeyStrictLikes {
					assert.True(t, strings.Contains(capturedQuery, "content.likes"), "strict:likes must order by content.likes")
				} else if rankType == variables.RedisKeyStrictViews {
					assert.True(t, strings.Contains(capturedQuery, "content.views"), "strict:views must order by content.views")
				}
			})
		}

		// 4. getPostsFromPostgresPaginated error paths (nil DB and query error)
		prevDB := infraPg.PostgresDB
		infraPg.PostgresDB = nil
		_, err = getPostsFromPostgresPaginated(ctx, variables.RedisKeyStrictLikes, 0, 5)
		infraPg.PostgresDB = prevDB
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeDatabaseError, appErr.Code)

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("pg query err")
		}, nil)
		_, err = getPostsFromPostgresPaginated(ctx, variables.RedisKeyStrictLikes, 0, 5)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeDatabaseError, appErr.Code)
	})

	t.Run("GetTagPosts L1 Hit and L2/L3 Fallbacks", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
		restore := test.ResetCollections()
		defer restore()

		post := post_models.PostPayload{ID: 701, Content: "Tagged Post", Visibility: 1}
		require.NoError(t, object_cache_service.SetPostInObjectCache(ctx, post))
		require.NoError(t, redis.TagPosts.ZAdd(ctx, "golang", 1000, "701"))

		// 1. L1 Hit
		res, err := GetTagPosts(ctx, "golang", 0, 10)
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, int64(701), res[0].ID)

		// 2. Deep offset -> L2 Hit
		mt.Run("L2 Hit", func(mt *mtest.T) {
			repoMongo.Posts = test.NewMockCollection(mt, "content.posts", nil)
			mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.content.posts", mtest.FirstBatch, bson.D{
				{Key: "id", Value: int64(702)},
				{Key: "content", Value: "L2 Tagged"},
				{Key: "visibility", Value: 1},
			}))

			res, err := GetTagPosts(ctx, "golang", int64(variables.MaxTagElements), 10)
			require.NoError(t, err)
			require.Len(t, res, 1)
			assert.Equal(t, int64(702), res[0].ID)
		})

		// 3. Deep offset -> L2 Error -> L3 Hit & L3 Error
		mt.Run("L3 Hit and L3 Error", func(mt *mtest.T) {
			repoMongo.Posts = test.NewMockCollection(mt, "content.posts", nil)
			mt.AddMockResponses(
				mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 1, Message: "mongo err"}),
				mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 1, Message: "mongo err"}),
			)

			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return test.NewMockRows([]string{"id"}, [][]driver.Value{{int64(701)}}), nil
			}, nil)

			res, err := GetTagPosts(ctx, "golang", int64(variables.MaxTagElements), 10)
			require.NoError(t, err)
			require.Len(t, res, 1)
			assert.Equal(t, int64(701), res[0].ID)

			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return nil, errors.New("pg tag err")
			}, nil)
			_, err = GetTagPosts(ctx, "golang", int64(variables.MaxTagElements), 10)
			var appErr *numan_error.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, numan_error.CodeDatabaseError, appErr.Code)
		})
	})
}
