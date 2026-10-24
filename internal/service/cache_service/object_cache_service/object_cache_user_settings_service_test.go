package object_cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/lib/pq"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestUserSettings_NormalizeAndL1Hit(t *testing.T) {
	srv := test.SetupTestRedis(t)
	ctx := context.Background()

	// Cache miss in getUserSettings returns 404 CodeNotFound AppError
	_, errMiss := getUserSettings(ctx, 9999)
	assertAppError(t, errMiss, http.StatusNotFound, numan_error.CodeNotFound)

	// Redis GET error in getUserSettings returns 500 CodeCacheError AppError
	srv.SetFailCmd("GET", errors.New("get err"))
	_, errGet := getUserSettings(ctx, 9999)
	assertAppError(t, errGet, http.StatusInternalServerError, numan_error.CodeCacheError)
	srv.SetFailCmd("GET", nil)

	// Out-of-range PostVisibilityDefault (e.g. legacy 99) should be normalized to PostVisibilityFriend
	s := user_settings_models.UserSettingsPayload{
		ID:     10,
		UserID: 100,
		Privacy: user_settings_models.PrivacySettings{
			PostVisibilityDefault: 99,
		},
	}

	if err := SetUserSettings(ctx, s); err != nil {
		t.Fatalf("SetUserSettings failed: %v", err)
	}

	got, err := GetUserSettingsCascade(ctx, 100)
	if err != nil {
		t.Fatalf("GetUserSettingsCascade (L1) failed: %v", err)
	}
	if got.ID != 10 || got.Privacy.PostVisibilityDefault != variables.PostVisibilityFriend {
		t.Errorf("expected normalized PostVisibilityDefault=%d, got %+v", variables.PostVisibilityFriend, got)
	}

	// Redis SET error
	srv.SetFailCmd("SET", errors.New("set err"))
	assertAppError(t, SetUserSettings(ctx, s), http.StatusInternalServerError, numan_error.CodeCacheError)
}

func TestGetUserSettingsCascade_L2MongoAndL3Postgres(t *testing.T) {
	ctx := context.Background()

	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("L2 Mongo hit and L3 Postgres fallback", func(mt *mtest.T) {
		test.SetupTestRedis(mt.T)
		restore := test.ResetCollections()
		defer restore()
		repoMongo.UserSettings = test.NewMockCollection(mt, "auth.user_settings", nil)

		// 1. L2 MongoDB hit ("auth.user_settings")
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.user_settings", mtest.FirstBatch, bson.D{
			{Key: "id", Value: int64(20)},
			{Key: "user_id", Value: int64(200)},
			{Key: "privacy", Value: bson.D{
				{Key: "post_visibility_default", Value: -5}, // invalid negative -> normalized to PostVisibilityFriend
			}},
		}))

		gotL2, err := GetUserSettingsCascade(ctx, 200)
		if err != nil {
			mt.Fatalf("GetUserSettingsCascade (L2) failed: %v", err)
		}
		if gotL2.ID != 20 || gotL2.Privacy.PostVisibilityDefault != variables.PostVisibilityFriend {
			mt.Errorf("unexpected L2 settings: %+v", gotL2)
		}

		// Verify L1 was rehydrated from L2
		cachedL1, err := getUserSettings(ctx, 200)
		if err != nil || cachedL1.ID != 20 {
			mt.Errorf("expected user 200 settings rehydrated in L1, got %+v (err=%v)", cachedL1, err)
		}

		// 2. L3 Postgres fallback when not in L1 or L2
		vecVal, _ := pq.Array([]float32{0.1, 0.2}).Value()
		tagsVal, _ := pq.Array([]string{"tech"}).Value()
		now := time.Now().UnixMilli()
		cols := []string{"id", "user_id", "privacy", "notifications", "display_and_content", "telemetry_vector", "telemetry_tags", "telemetry_timestamp"}

		returnEmptyPg := false
		failPg := false
		test.SetupMockDB(mt.T, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if failPg {
				return nil, errors.New("pg down")
			}
			if returnEmptyPg {
				return test.NewMockRows(cols, nil), nil
			}
			return test.NewMockRows(cols, [][]driver.Value{{
				int64(30),
				int64(300),
				[]byte(`{"post_visibility_default":0,"show_online_status":true}`),
				[]byte(`{"master_push_enabled":true,"notify_messages":true}`),
				[]byte(`{"theme":1,"language":2}`),
				vecVal,
				tagsVal,
				now,
			}}), nil
		}, nil)

		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.user_settings", mtest.FirstBatch))
		gotL3, err := GetUserSettingsCascade(ctx, 300)
		if err != nil {
			mt.Fatalf("GetUserSettingsCascade (L3) failed: %v", err)
		}
		if gotL3.ID != 30 || gotL3.UserID != 300 || !gotL3.Notifications.MasterPushEnabled || gotL3.DisplayAndContent.Theme != 1 {
			mt.Errorf("unexpected L3 settings: %+v", gotL3)
		}

		// 3. Not found when L3 returns 0 rows (ID == 0)
		returnEmptyPg = true
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.user_settings", mtest.FirstBatch))
		_, err = GetUserSettingsCascade(ctx, 404)
		assertAppError(mt, err, http.StatusNotFound, numan_error.CodeNotFound)

		// 4. Internal error when L3 fails
		failPg = true
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.user_settings", mtest.FirstBatch))
		_, err = GetUserSettingsCascade(ctx, 500)
		assertAppError(mt, err, http.StatusInternalServerError, numan_error.CodeDatabaseError)
	})
}
