package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
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

func TestSpeedCacheUsersService(t *testing.T) {
	ctx := context.Background()

	t.Run("storeUserLiteInSpeedCache, AddUserToSpeedCache, UpdateUserSpeedCachePrivacy, SearchUserByPrefix", func(t *testing.T) {
		srv := test.SetupTestRedis(t)

		u1 := lite_models.UserLiteRequest{ID: 1, Username: "Alice"}
		require.NoError(t, storeUserLiteInSpeedCache(ctx, u1))

		u2Payload := auth_models.UserPayload{ID: 2, Username: "Alex", FirstName: "Alex"}
		u2Settings := user_settings_models.UserSettingsPayload{
			Privacy: user_settings_models.PrivacySettings{ConversationPermission: 1, AddGroupPermission: 2, HideConnections: 1},
		}
		require.NoError(t, AddUserToSpeedCache(ctx, u2Payload, u2Settings))

		// Update privacy
		require.NoError(t, UpdateUserSpeedCachePrivacy(ctx, 2, 0, 0, 0))
		var updated lite_models.UserLiteRequest
		require.NoError(t, redis.UsersLite.GetObject(ctx, 2, &updated))
		assert.Equal(t, 0, updated.ConversationPermission)

		// Search by prefix
		results, err := SearchUserByPrefix(ctx, "al", 0, 10)
		require.NoError(t, err)
		require.Len(t, results, 2)
		assert.Equal(t, int64(2), results[0].ID) // "alex:2" < "alice:1"
		assert.Equal(t, int64(1), results[1].ID)

		empty, err := SearchUserByPrefix(ctx, "zzz", 0, 10)
		require.NoError(t, err)
		assert.Empty(t, empty)

		// Corrupted msgpack skipped during search
		require.NoError(t, redis.UsersLex.ZAdd(ctx, variables.LexicographicGlobalKey, 0, "alcorrupt:3"))
		require.NoError(t, redis.UsersLite.SetPrimitive(ctx, 3, "corrupt-msgpack"))
		results, err = SearchUserByPrefix(ctx, "al", 0, 10)
		require.NoError(t, err)
		assert.Len(t, results, 2)

		// Error branches
		var appErr *numan_error.AppError
		srv.SetFailCmd("ZADD", errors.New("zadd warn"))
		require.NoError(t, storeUserLiteInSpeedCache(ctx, u1))
		require.NoError(t, AddUserToSpeedCache(ctx, u2Payload, u2Settings))
		srv.SetFailCmd("ZADD", nil)

		srv.SetFailCmd("SET", errors.New("set err"))
		require.ErrorAs(t, storeUserLiteInSpeedCache(ctx, u1), &appErr)
		require.ErrorAs(t, AddUserToSpeedCache(ctx, u2Payload, u2Settings), &appErr)
		require.ErrorAs(t, UpdateUserSpeedCachePrivacy(ctx, 2, 1, 1, 1), &appErr)
		srv.SetFailCmd("SET", nil)

		srv.SetFailCmd("GET", errors.New("get warn"))
		require.NoError(t, UpdateUserSpeedCachePrivacy(ctx, 2, 1, 1, 1))
		srv.SetFailCmd("GET", nil)

		srv.SetFailCmd("ZRANGEBYLEX", errors.New("lex err"))
		_, err = SearchUserByPrefix(ctx, "al", 0, 10)
		require.ErrorAs(t, err, &appErr)
		srv.SetFailCmd("ZRANGEBYLEX", nil)

		srv.SetFailCmd("MGET", errors.New("mget err"))
		_, err = SearchUserByPrefix(ctx, "al", 0, 10)
		require.ErrorAs(t, err, &appErr)
	})

	t.Run("GetUserLite Cascade L1 -> L2 -> L3", func(t *testing.T) {
		_ = test.SetupTestRedis(t)
		mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
		restore := test.ResetCollections()
		defer restore()

		// 1. L1 Hit
		uL1 := lite_models.UserLiteRequest{ID: 10, Username: "L1User"}
		require.NoError(t, redis.UsersLite.SetObject(ctx, 10, uL1))
		got, err := GetUserLite(ctx, 10)
		require.NoError(t, err)
		assert.Equal(t, "L1User", got.Username)

		// 2. L1 Miss -> L2 Mongo Hit
		mt.Run("L2 Mongo Hit", func(mt *mtest.T) {
			repoMongo.Users = test.NewMockCollection(mt, "auth.users", nil)
			repoMongo.UserSettings = test.NewMockCollection(mt, "auth.user_settings", nil)
			mt.AddMockResponses(
				mtest.CreateCursorResponse(0, "numan_mongo.auth.users", mtest.FirstBatch, bson.D{
					{Key: "id", Value: int64(20)},
					{Key: "username", Value: "L2User"},
				}),
				mtest.CreateCursorResponse(0, "numan_mongo.auth.user_settings", mtest.FirstBatch, bson.D{
					{Key: "id", Value: int64(1)},
					{Key: "user_id", Value: int64(20)},
				}),
			)

			got, err := GetUserLite(ctx, 20)
			require.NoError(t, err)
			assert.Equal(t, "L2User", got.Username)
		})

		// 3. L1 & L2 Miss -> L3 Postgres Hit
		mt.Run("L3 Postgres Hit", func(mt *mtest.T) {
			repoMongo.Users = test.NewMockCollection(mt, "auth.users", nil)
			repoMongo.UserSettings = test.NewMockCollection(mt, "auth.user_settings", nil)
			mt.AddMockResponses(
				mtest.CreateCursorResponse(0, "numan_mongo.auth.users", mtest.FirstBatch),
				mtest.CreateCursorResponse(0, "numan_mongo.auth.user_settings", mtest.FirstBatch),
				mtest.CreateSuccessResponse(),
			)

			now := time.Now()
			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "func_load_user_settings") {
					return test.NewMockRows(
						[]string{"id", "user_id", "privacy", "notifications", "display_and_content", "telemetry_vector", "telemetry_tags", "telemetry_timestamp"},
						[][]driver.Value{{int64(1), int64(30), []byte(`{}`), []byte(`{}`), []byte(`{}`), `{}`, `{}`, int64(0)}},
					), nil
				}
				return test.NewMockRows(
					[]string{
						"id", "username", "email", "email_verified", "phone", "phone_verified",
						"password_hash", "first_name", "last_name", "birthdate", "sex", "bio",
						"profile_picture_id", "grade", "location", "school", "work", "badges",
						"desactivated", "banned", "ban_reason", "ban_expires_at", "created_at", "updated_at",
						"two_factor_enabled", "two_factor_method", "totp_secret", "trust_score", "last_infraction_at",
					},
					[][]driver.Value{{
						int64(30), "L3User", "l3@test.com", true, "", false, "hash", "First", "Last", nil, 0, "Bio",
						int64(0), 0, "", "", "", []byte(`{}`), false, false, nil, nil, now, now, false, 0, nil, float64(0.55), int64(0),
					}},
				), nil
			}, nil)

			got, err := GetUserLite(ctx, 30)
			require.NoError(t, err)
			assert.Equal(t, "L3User", got.Username)
			time.Sleep(20 * time.Millisecond)
		})

		// 4. L3 Not Found (empty rows)
		mt.Run("L3 Not Found", func(mt *mtest.T) {
			repoMongo.Users = test.NewMockCollection(mt, "auth.users", nil)
			mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.users", mtest.FirstBatch))

			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return test.NewMockRows([]string{"id"}, nil), nil
			}, nil)

			_, err := GetUserLite(ctx, 404)
			var appErr *numan_error.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
		})

		// 5. L3 Error propagated
		mt.Run("L3 Error", func(mt *mtest.T) {
			repoMongo.Users = test.NewMockCollection(mt, "auth.users", nil)
			mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.users", mtest.FirstBatch))

			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return nil, errors.New("pg down")
			}, nil)

			_, err := GetUserLite(ctx, 500)
			var appErr *numan_error.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, numan_error.CodeDatabaseError, appErr.Code)
		})
	})
}
