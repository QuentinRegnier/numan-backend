package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAddableUsersFromSpeedCache(t *testing.T) {
	ctx := context.Background()

	t.Run("L1 Cache Hit returns sorted users and skips empty marker", func(t *testing.T) {
		_ = test.SetupTestRedis(t)

		u1 := lite_models.UserLiteRequest{ID: 101, Username: "Alice"}
		u2 := lite_models.UserLiteRequest{ID: 102, Username: "Bob"}
		require.NoError(t, redis.UsersLite.SetObject(ctx, u1.ID, u1))
		require.NoError(t, redis.UsersLite.SetObject(ctx, u2.ID, u2))

		zsetKey := redis.SpeedAddable.Key(1)
		require.NoError(t, redis.ZAdd(ctx, zsetKey, 0, "0_alice_101"))
		require.NoError(t, redis.ZAdd(ctx, zsetKey, 0, "1_bob_102"))
		require.NoError(t, redis.ZAdd(ctx, zsetKey, 0, variables.SpeedCacheEmptyMarker))

		res, err := GetAddableUsersFromSpeedCache(ctx, 1, 10, 0, false)
		require.NoError(t, err)
		require.Len(t, res, 2)
		assert.Equal(t, int64(101), res[0].ID)
		assert.Equal(t, int64(102), res[1].ID)
	})

	t.Run("Cache Miss rebuilds from Postgres with Friends before Followers", func(t *testing.T) {
		_ = test.SetupTestRedis(t)

		uFriend := lite_models.UserLiteRequest{ID: 201, Username: "Zoe"}
		uFollow := lite_models.UserLiteRequest{ID: 202, Username: "Adam"}
		require.NoError(t, redis.UsersLite.SetObject(ctx, uFriend.ID, uFriend))
		require.NoError(t, redis.UsersLite.SetObject(ctx, uFollow.ID, uFollow))
		// Also store corrupted msgpack for user 203 to test unmarshal warning
		require.NoError(t, redis.UsersLite.SetPrimitive(ctx, 203, "invalid-msgpack"))

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(
				[]string{"target_id", "state"},
				[][]driver.Value{
					{int64(202), variables.RelationStateFollow},
					{int64(201), variables.RelationStateFriend},
					{int64(203), variables.RelationStateFriend},
					{int64(204), variables.RelationStateFriend}, // missing in UsersLite
				},
			), nil
		}, nil)

		res, err := GetAddableUsersFromSpeedCache(ctx, 10, 10, 0, true)
		require.NoError(t, err)
		require.Len(t, res, 2)
		// Friend (state 2 -> inverted 0) comes before Follow (state 1 -> inverted 1)
		assert.Equal(t, int64(201), res[0].ID)
		assert.Equal(t, int64(202), res[1].ID)
	})

	t.Run("Cache Miss with empty Postgres result sets empty marker", func(t *testing.T) {
		_ = test.SetupTestRedis(t)

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows([]string{"target_id", "state"}, nil), nil
		}, nil)

		res, err := GetAddableUsersFromSpeedCache(ctx, 20, 10, 0, false)
		require.NoError(t, err)
		assert.Empty(t, res)
	})

	t.Run("Error Paths", func(t *testing.T) {
		t.Run("Redis Exists failure", func(t *testing.T) {
			srv := test.SetupTestRedis(t)
			srv.SetFailCmd("DEL", errors.New("del err"))
			srv.SetFailCmd("EXISTS", errors.New("exists err"))

			_, err := GetAddableUsersFromSpeedCache(ctx, 1, 10, 0, true)
			var appErr *numan_error.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, http.StatusInternalServerError, appErr.HTTPStatus)
			assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		})

		t.Run("Postgres error propagated directly", func(t *testing.T) {
			_ = test.SetupTestRedis(t)
			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return nil, errors.New("pg down")
			}, nil)

			_, err := GetAddableUsersFromSpeedCache(ctx, 1, 10, 0, false)
			var appErr *numan_error.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, numan_error.CodeDatabaseError, appErr.Code)
		})

		t.Run("ZAdd failure on non-empty rebuild", func(t *testing.T) {
			srv := test.SetupTestRedis(t)
			u := lite_models.UserLiteRequest{ID: 301, Username: "Alice"}
			require.NoError(t, redis.UsersLite.SetObject(ctx, u.ID, u))

			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return test.NewMockRows([]string{"target_id", "state"}, [][]driver.Value{{int64(301), variables.RelationStateFriend}}), nil
			}, nil)

			srv.SetFailCmd("ZADD", errors.New("zadd fail"))
			_, err := GetAddableUsersFromSpeedCache(ctx, 30, 10, 0, false)
			var appErr *numan_error.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		})

		t.Run("Expire failure on non-empty rebuild", func(t *testing.T) {
			srv := test.SetupTestRedis(t)
			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return test.NewMockRows([]string{"target_id", "state"}, [][]driver.Value{{int64(301), variables.RelationStateFriend}}), nil
			}, nil)

			srv.SetFailCmd("MGET", errors.New("mget fail"))
			srv.SetFailCmd("EXPIRE", errors.New("expire fail"))
			_, err := GetAddableUsersFromSpeedCache(ctx, 31, 10, 0, false)
			var appErr *numan_error.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		})

		t.Run("ZAdd and Expire failure on empty rebuild", func(t *testing.T) {
			srv := test.SetupTestRedis(t)
			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return test.NewMockRows([]string{"target_id", "state"}, nil), nil
			}, nil)

			srv.SetFailCmd("ZADD", errors.New("zadd empty fail"))
			_, err := GetAddableUsersFromSpeedCache(ctx, 32, 10, 0, false)
			var appErr *numan_error.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, numan_error.CodeCacheError, appErr.Code)

			srv.SetFailCmd("ZADD", nil)
			srv.SetFailCmd("EXPIRE", errors.New("expire empty fail"))
			_, err = GetAddableUsersFromSpeedCache(ctx, 33, 10, 0, false)
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		})

		t.Run("ZRange failure and Final MGet failure and Corrupted final msgpack", func(t *testing.T) {
			srv := test.SetupTestRedis(t)
			zsetKey := redis.SpeedAddable.Key(40)
			require.NoError(t, redis.ZAdd(ctx, zsetKey, 0, "0_alice_401"))

			srv.SetFailCmd("ZRANGE", errors.New("zrange fail"))
			_, err := GetAddableUsersFromSpeedCache(ctx, 40, 10, 0, false)
			var appErr *numan_error.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, numan_error.CodeCacheError, appErr.Code)

			srv.SetFailCmd("ZRANGE", nil)
			srv.SetFailCmd("MGET", errors.New("mget final fail"))
			_, err = GetAddableUsersFromSpeedCache(ctx, 40, 10, 0, false)
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, numan_error.CodeCacheError, appErr.Code)

			srv.SetFailCmd("MGET", nil)
			require.NoError(t, redis.UsersLite.SetPrimitive(ctx, 401, "bad-msgpack"))
			res, err := GetAddableUsersFromSpeedCache(ctx, 40, 10, 0, false)
			require.NoError(t, err)
			assert.Empty(t, res)
		})
	})
}
