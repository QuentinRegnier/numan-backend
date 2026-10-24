package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestSessionCacheService(t *testing.T) {
	ctx := context.Background()

	t.Run("SetSessionInCache, LoadSessionFromCache, DeleteSessionFromCache", func(t *testing.T) {
		srv := test.SetupTestRedis(t)

		sess := auth_models.SessionsPayload{
			ID:                     100,
			UserID:                 10,
			FirebaseInstallationID: "fid-abc",
			MasterToken:            "secret-master",
		}
		// Test nil context fallback in getShortCtx
		//nolint:staticcheck
		require.NoError(t, SetSessionInCache(nil, sess))

		// Load valid session
		loaded, err := LoadSessionFromCache(ctx, 10, "fid-abc", "secret-master")
		require.NoError(t, err)
		assert.Equal(t, int64(100), loaded.ID)

		// Invalid master token returns Forbidden
		_, err = LoadSessionFromCache(ctx, 10, "fid-abc", "wrong-token")
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)

		// Delete session sets Tombstone and deletes index & session object
		require.NoError(t, DeleteSessionFromCache(ctx, 100, 10, "fid-abc"))

		// Loading after delete returns NotFound
		_, err = LoadSessionFromCache(ctx, 10, "fid-abc", "")
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)

		// Error paths in Set, Load, Delete
		srv.SetFailCmd("SET", errors.New("set err"))
		require.ErrorAs(t, SetSessionInCache(ctx, sess), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)

		require.ErrorAs(t, DeleteSessionFromCache(ctx, 100, 10, "fid-abc"), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("SET", nil)

		// Re-populate index only, fail GET on session object
		require.NoError(t, redis.SessionIndexes.SetPrimitive(ctx, "10:fid-abc", 100))
		srv.SetFailCmd("GET", errors.New("get err"))
		_, err = LoadSessionFromCache(ctx, 10, "fid-abc", "")
		require.ErrorAs(t, err, &appErr)
		srv.SetFailCmd("GET", nil)

		// DEL failure in DeleteSessionFromCache
		srv.SetFailCmd("DEL", errors.New("del err"))
		require.ErrorAs(t, DeleteSessionFromCache(ctx, 100, 10, "fid-abc"), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		require.ErrorAs(t, DeleteSessionFromCache(ctx, 100, 0, ""), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("DEL", nil)
	})

	t.Run("GetFirebaseInstallationIDsCascade L1 -> L2 -> L3", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
		restore := test.ResetCollections()
		defer restore()

		// 1. L1 Hit
		require.NoError(t, redis.SessionIndexes.SAdd(ctx, 10, "fcm-1", "fcm-2"))
		tokens, err := GetFirebaseInstallationIDsCascade(ctx, 10)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"fcm-1", "fcm-2"}, tokens)

		// 2. L1 Miss -> L2 Mongo Hit + L1 Self-Healing
		mt.Run("L2 Mongo Hit", func(mt *mtest.T) {
			repoMongo.Sessions = test.NewMockCollection(mt, "auth.sessions", nil)
			mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.sessions", mtest.FirstBatch, bson.D{
				{Key: "id", Value: int64(200)},
				{Key: "user_id", Value: int64(20)},
				{Key: "firebase_installation_id", Value: "fcm-mongo"},
			}))

			tokens, err := GetFirebaseInstallationIDsCascade(ctx, 20)
			require.NoError(t, err)
			assert.Equal(t, []string{"fcm-mongo"}, tokens)
			time.Sleep(20 * time.Millisecond)
		})

		// 3. L1 & L2 Miss -> L3 Postgres Hit + Promotion
		mt.Run("L3 Postgres Hit", func(mt *mtest.T) {
			repoMongo.Sessions = test.NewMockCollection(mt, "auth.sessions", nil)
			mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.sessions", mtest.FirstBatch))

			now := time.Now().UnixMilli()
			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return test.NewMockRows(
					[]string{"id", "user_id", "master_token", "firebase_installation_id", "device_info", "ip_history", "created_at", "expires_at"},
					[][]driver.Value{{int64(300), int64(30), "tok", "fcm-pg", []byte(`{}`), `{127.0.0.1}`, now, now}},
				), nil
			}, nil)

			tokens, err := GetFirebaseInstallationIDsCascade(ctx, 30)
			require.NoError(t, err)
			assert.Equal(t, []string{"fcm-pg"}, tokens)
			time.Sleep(20 * time.Millisecond)
		})

		// 4. Complete Miss -> NotFound
		mt.Run("Complete Miss", func(mt *mtest.T) {
			repoMongo.Sessions = test.NewMockCollection(mt, "auth.sessions", nil)
			mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.sessions", mtest.FirstBatch))

			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return test.NewMockRows(
					[]string{"id", "user_id", "master_token", "firebase_installation_id", "device_info", "ip_history", "created_at", "expires_at"},
					nil,
				), nil
			}, nil)

			srv.SetFailCmd("SMEMBERS", errors.New("smembers warn"))
			_, err := GetFirebaseInstallationIDsCascade(ctx, 404)
			srv.SetFailCmd("SMEMBERS", nil)
			var appErr *numan_error.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
		})

		// 5. L3 Error propagated directly
		mt.Run("L3 Error", func(mt *mtest.T) {
			repoMongo.Sessions = test.NewMockCollection(mt, "auth.sessions", nil)
			mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 1, Message: "mongo err"}))

			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return nil, errors.New("pg down")
			}, nil)

			_, err := GetFirebaseInstallationIDsCascade(ctx, 500)
			var appErr *numan_error.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, numan_error.CodeDatabaseError, appErr.Code)
		})
	})
}
