package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/lib/pq"
	"go.mongodb.org/mongo-driver/bson"
)

func TestSessionCacheLifecycle(t *testing.T) {
	ctx := context.Background()

	t.Run("SetSessionInCache stores session and composite index, LoadSessionFromCache validates master token", func(t *testing.T) {
		setupTestRedis(t)

		sess := auth_models.SessionsPayload{
			ID:                     9001,
			UserID:                 42,
			MasterToken:            "secret_master_token",
			FirebaseInstallationID: "fid_device_1",
			CurrentSecret:          "ratchet_1",
		}

		// Also verify nil parent context is safely handled by getShortCtx
		if err := SetSessionInCache(nil, sess); err != nil {
			t.Fatalf("SetSessionInCache(nil) unexpected error: %v", err)
		}

		// Load with matching master token
		loaded, err := LoadSessionFromCache(ctx, 42, "fid_device_1", "secret_master_token")
		if err != nil {
			t.Fatalf("LoadSessionFromCache() unexpected error: %v", err)
		}
		if loaded.ID != sess.ID || loaded.MasterToken != sess.MasterToken {
			t.Errorf("LoadSessionFromCache() = %+v, want %+v", loaded, sess)
		}

		// Load with empty master token (skips token comparison)
		loadedNoTok, err := LoadSessionFromCache(ctx, 42, "fid_device_1", "")
		if err != nil || loadedNoTok.ID != sess.ID {
			t.Errorf("LoadSessionFromCache(empty masterToken) = (%+v, %v), want session 9001", loadedNoTok, err)
		}

		// Load with mismatched master token -> 403 Forbidden
		_, errForbidden := LoadSessionFromCache(ctx, 42, "fid_device_1", "wrong_token")
		var appErr *numan_error.AppError
		if !errors.As(errForbidden, &appErr) || appErr.HTTPStatus != http.StatusForbidden {
			t.Errorf("LoadSessionFromCache(wrong_token) error = %v, want 403 Forbidden AppError", errForbidden)
		}
	})

	t.Run("LoadSessionFromCache returns 404 when index is missing or userID is -1", func(t *testing.T) {
		setupTestRedis(t)

		_, err := LoadSessionFromCache(ctx, -1, "fid_1", "tok")
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusNotFound {
			t.Errorf("LoadSessionFromCache(userID=-1) error = %v, want 404 NotFound", err)
		}

		_, err2 := LoadSessionFromCache(ctx, 42, "unknown_fid", "tok")
		if !errors.As(err2, &appErr) || appErr.HTTPStatus != http.StatusNotFound {
			t.Errorf("LoadSessionFromCache(unknown_fid) error = %v, want 404 NotFound", err2)
		}
	})

	t.Run("LoadSessionFromCache returns 500 when index exists but session object is missing", func(t *testing.T) {
		setupTestRedis(t)
		_ = repoRedis.SessionIndexes.SetPrimitive(ctx, "42:orphan_fid", int64(9999))

		_, err := LoadSessionFromCache(ctx, 42, "orphan_fid", "")
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("LoadSessionFromCache(orphan_fid) error = %v, want 500 InternalServerError", err)
		}
	})

	t.Run("DeleteSessionFromCache sets blacklist tombstone, removes index, and deletes session object", func(t *testing.T) {
		setupTestRedis(t)
		sess := auth_models.SessionsPayload{
			ID:                     9002,
			UserID:                 50,
			FirebaseInstallationID: "fid_del",
			MasterToken:            "tok",
		}
		_ = SetSessionInCache(ctx, sess)

		if err := DeleteSessionFromCache(ctx, 9002, 50, "fid_del"); err != nil {
			t.Fatalf("DeleteSessionFromCache() unexpected error: %v", err)
		}

		// Verify tombstone exists in SessionBlacklist
		blacklisted, err := repoRedis.SessionBlacklist.Exists(ctx, "50:fid_del")
		if err != nil || !blacklisted {
			t.Errorf("expected SessionBlacklist tombstone for 50:fid_del, got (%v, %v)", blacklisted, err)
		}

		// Verify session can no longer be loaded
		if _, err := LoadSessionFromCache(ctx, 50, "fid_del", ""); err == nil {
			t.Error("expected LoadSessionFromCache to fail after DeleteSessionFromCache")
		}
	})

	t.Run("error propagation on SetSessionInCache and DeleteSessionFromCache", func(t *testing.T) {
		srv := setupTestRedis(t)

		srv.setFailCmd("SET", errors.New("set error"))
		if err := SetSessionInCache(ctx, auth_models.SessionsPayload{ID: 1}); err == nil {
			t.Error("SetSessionInCache() expected error on SET failure")
		}
		if err := DeleteSessionFromCache(ctx, 1, 10, "fid"); err == nil {
			t.Error("DeleteSessionFromCache() expected error on tombstone SET failure")
		}
		srv.setFailCmd("SET", nil)

		srv.setFailCmd("DEL", errors.New("del error"))
		if err := DeleteSessionFromCache(ctx, 1, 10, "fid"); err == nil {
			t.Error("DeleteSessionFromCache() expected error on DEL failure")
		}
		if err := DeleteSessionFromCache(ctx, 1, 0, ""); err == nil {
			t.Error("DeleteSessionFromCache(no index) expected error on DEL failure")
		}
		srv.setFailCmd("DEL", nil)
	})
}

func TestGetFirebaseInstallationIDsCascade(t *testing.T) {
	ctx := context.Background()

	t.Run("L1 cache hit returns tokens from SessionIndexes SMembers", func(t *testing.T) {
		setupTestRedis(t)
		_ = repoRedis.SessionIndexes.SAdd(ctx, int64(100), "fcm_1", "fcm_2")

		tokens, err := GetFirebaseInstallationIDsCascade(ctx, 100)
		if err != nil {
			t.Fatalf("GetFirebaseInstallationIDsCascade() L1 error: %v", err)
		}
		want := []string{"fcm_1", "fcm_2"}
		if !reflect.DeepEqual(tokens, want) {
			t.Errorf("GetFirebaseInstallationIDsCascade() L1 = %v, want %v", tokens, want)
		}
	})

	t.Run("L1 miss -> L2 MongoDB hit returns tokens and heals L1 asynchronously", func(t *testing.T) {
		setupTestRedis(t)
		mSrv := setupTestMongo(t)
		mSrv.setDocs("auth.sessions",
			bson.M{"user_id": int64(101), "firebase_installation_id": "mongo_fcm_1"},
			bson.M{"user_id": int64(101), "firebase_installation_id": "mongo_fcm_2"},
		)

		tokens, err := GetFirebaseInstallationIDsCascade(ctx, 101)
		if err != nil {
			t.Fatalf("GetFirebaseInstallationIDsCascade() L2 error: %v", err)
		}
		want := []string{"mongo_fcm_1", "mongo_fcm_2"}
		if !reflect.DeepEqual(tokens, want) {
			t.Errorf("GetFirebaseInstallationIDsCascade() L2 = %v, want %v", tokens, want)
		}

		time.Sleep(25 * time.Millisecond)
		healed, _ := repoRedis.SessionIndexes.SMembers(ctx, int64(101))
		if !reflect.DeepEqual(healed, want) {
			t.Errorf("expected L1 SessionIndexes to be healed with %v, got %v", want, healed)
		}
	})

	t.Run("L1 & L2 miss -> L3 Postgres hit returns tokens and promotes to L1/L2", func(t *testing.T) {
		setupTestRedis(t)
		mSrv := setupTestMongo(t)
		mSrv.setDocs("auth.sessions") // empty L2

		ipVal, _ := pq.StringArray{"127.0.0.1"}.Value()
		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{
				cols: []string{"id", "user_id", "master_token", "firebase_installation_id", "device_info", "ip_history", "created_at", "expires_at"},
				rows: [][]driver.Value{
					{int64(701), int64(102), "mtok", "pg_fcm_1", []byte(`{"os":"ios"}`), ipVal, int64(1000), int64(2000)},
				},
			}
		})

		tokens, err := GetFirebaseInstallationIDsCascade(ctx, 102)
		if err != nil {
			t.Fatalf("GetFirebaseInstallationIDsCascade() L3 error: %v", err)
		}
		if !reflect.DeepEqual(tokens, []string{"pg_fcm_1"}) {
			t.Errorf("GetFirebaseInstallationIDsCascade() L3 = %v, want [pg_fcm_1]", tokens)
		}

		time.Sleep(25 * time.Millisecond)
		var promotedSess auth_models.SessionsPayload
		if err := repoRedis.Sessions.GetObject(ctx, int64(701), &promotedSess); err != nil || promotedSess.ID != 701 {
			t.Errorf("expected session 701 to be promoted to L1, got (%+v, %v)", promotedSess, err)
		}
	})

	t.Run("L3 error returns 500 and empty L3 result returns 404", func(t *testing.T) {
		setupTestRedis(t)
		mSrv := setupTestMongo(t)
		mSrv.setDocs("auth.sessions")

		// Empty rows -> 404 NotFound
		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{
				cols: []string{"id", "user_id", "master_token", "firebase_installation_id", "device_info", "ip_history", "created_at", "expires_at"},
				rows: nil,
			}
		})

		_, err404 := GetFirebaseInstallationIDsCascade(ctx, 404)
		var appErr *numan_error.AppError
		if !errors.As(err404, &appErr) || appErr.HTTPStatus != http.StatusNotFound {
			t.Errorf("GetFirebaseInstallationIDsCascade(empty) error = %v, want 404 NotFound", err404)
		}

		// SQL error -> 500 InternalServerError
		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{err: errors.New("db error")}
		})

		_, err500 := GetFirebaseInstallationIDsCascade(ctx, 500)
		if !errors.As(err500, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("GetFirebaseInstallationIDsCascade(db error) error = %v, want 500 InternalServerError", err500)
		}
	})
}
