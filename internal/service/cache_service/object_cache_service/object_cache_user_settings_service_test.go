package object_cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/lib/pq"
	"go.mongodb.org/mongo-driver/bson"
)

func TestUserSettings_NormalizeAndL1Hit(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

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
	srv.setFailCmd("SET", errors.New("set err"))
	if err := SetUserSettings(ctx, s); err == nil {
		t.Errorf("expected error when SET fails, got nil")
	}
}

func TestGetUserSettingsCascade_L2MongoAndL3Postgres(t *testing.T) {
	setupTestRedis(t)
	mongoSrv := setupTestMongo(t)
	ctx := context.Background()

	// 1. L2 MongoDB hit ("auth.user_settings")
	mongoSrv.setDocs("auth.user_settings", bson.M{
		"id":      int64(20),
		"user_id": int64(200),
		"privacy": bson.M{
			"post_visibility_default": -5, // invalid negative -> normalized to PostVisibilityFriend
		},
	})

	gotL2, err := GetUserSettingsCascade(ctx, 200)
	if err != nil {
		t.Fatalf("GetUserSettingsCascade (L2) failed: %v", err)
	}
	if gotL2.ID != 20 || gotL2.Privacy.PostVisibilityDefault != variables.PostVisibilityFriend {
		t.Errorf("unexpected L2 settings: %+v", gotL2)
	}

	// Verify L1 was rehydrated from L2
	cachedL1, err := getUserSettings(ctx, 200)
	if err != nil || cachedL1.ID != 20 {
		t.Errorf("expected user 200 settings rehydrated in L1, got %+v (err=%v)", cachedL1, err)
	}

	// 2. L3 Postgres fallback when not in L1 or L2
	mongoSrv.setDocs("auth.user_settings") // empty L2
	vecVal, _ := pq.Array([]float32{0.1, 0.2}).Value()
	tagsVal, _ := pq.Array([]string{"tech"}).Value()
	now := time.Now().UnixMilli()

	returnEmptyPg := false
	failPg := false
	setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
		if failPg {
			return fakeSQLResponse{err: errors.New("pg down")}
		}
		if returnEmptyPg {
			return fakeSQLResponse{
				cols: []string{"id", "user_id", "privacy", "notifications", "display_and_content", "telemetry_vector", "telemetry_tags", "telemetry_timestamp"},
				rows: [][]driver.Value{},
			}
		}
		return fakeSQLResponse{
			cols: []string{"id", "user_id", "privacy", "notifications", "display_and_content", "telemetry_vector", "telemetry_tags", "telemetry_timestamp"},
			rows: [][]driver.Value{{
				int64(30),
				int64(300),
				[]byte(`{"post_visibility_default":0,"show_online_status":true}`),
				[]byte(`{"master_push_enabled":true,"notify_messages":true}`),
				[]byte(`{"theme":1,"language":2}`),
				vecVal,
				tagsVal,
				now,
			}},
		}
	})

	gotL3, err := GetUserSettingsCascade(ctx, 300)
	if err != nil {
		t.Fatalf("GetUserSettingsCascade (L3) failed: %v", err)
	}
	if gotL3.ID != 30 || gotL3.UserID != 300 || !gotL3.Notifications.MasterPushEnabled || gotL3.DisplayAndContent.Theme != 1 {
		t.Errorf("unexpected L3 settings: %+v", gotL3)
	}

	// 3. Not found when L3 returns 0 rows (ID == 0)
	returnEmptyPg = true
	if _, err := GetUserSettingsCascade(ctx, 404); err == nil {
		t.Errorf("expected NotFound error when user settings do not exist in L1/L2/L3, got nil")
	}

	// 4. Internal error when L3 fails
	failPg = true
	if _, err := GetUserSettingsCascade(ctx, 500); err == nil {
		t.Errorf("expected error when Postgres fails, got nil")
	}
}
