package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/auth_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_settings_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"go.mongodb.org/mongo-driver/bson"
)

func TestSpeedCacheUsersOperations(t *testing.T) {
	ctx := context.Background()

	t.Run("AddUserToSpeedCache, UpdateUserSpeedCachePrivacy, and SearchUserByPrefix", func(t *testing.T) {
		setupTestRedis(t)

		u1 := auth_models.UserPayload{
			ID:        10,
			Username:  "Charlie",
			FirstName: "Charles",
			LastName:  "Dupont",
			Grade:     1,
			Badges:    nil, // nil badges should be initialized to non-nil empty map
		}
		s1 := user_settings_models.UserSettingsPayload{
			UserID: 10,
			Privacy: user_settings_models.PrivacySettings{
				ConversationPermission: 1,
				AddGroupPermission:     2,
				HideConnections:        1,
				ShowOnlineStatus:       true,
			},
		}

		if err := AddUserToSpeedCache(ctx, u1, s1); err != nil {
			t.Fatalf("AddUserToSpeedCache(10) error: %v", err)
		}

		u2 := lite_models.UserLiteRequest{ID: 20, Username: "Charlotte", FirstName: "Charlotte"}
		if err := storeUserLiteInSpeedCache(ctx, u2); err != nil {
			t.Fatalf("storeUserLiteInSpeedCache(20) error: %v", err)
		}

		// Search prefix "CHAR" -> should return Charlie (10) then Charlotte (20)
		found, err := SearchUserByPrefix(ctx, "CHAR", 0, 10)
		if err != nil {
			t.Fatalf("SearchUserByPrefix(\"CHAR\") error: %v", err)
		}
		if len(found) != 2 || found[0].ID != 10 || found[1].ID != 20 {
			t.Fatalf("SearchUserByPrefix(\"CHAR\") = %+v, want IDs [10, 20]", found)
		}
		if found[0].Badges == nil {
			t.Errorf("expected non-nil Badges map on hydrated user 10")
		}

		// Update privacy settings in speed cache
		if err := UpdateUserSpeedCachePrivacy(ctx, 10, 3, 0, 2); err != nil {
			t.Fatalf("UpdateUserSpeedCachePrivacy(10) error: %v", err)
		}

		lite10, err := GetUserLite(ctx, 10)
		if err != nil {
			t.Fatalf("GetUserLite(10) error: %v", err)
		}
		if lite10.ConversationPermission != 3 || lite10.AddGroupPermission != 0 || lite10.HideConnections != 2 {
			t.Errorf("GetUserLite(10) privacy after update = %+v, want (3, 0, 2)", lite10)
		}
	})

	t.Run("GetUserLite L1 miss -> L2 MongoDB hit rehydrates L1 and combines user settings", func(t *testing.T) {
		setupTestRedis(t)
		mSrv := setupTestMongo(t)

		// Put user settings in L1 so GetUserSettingsCascade succeeds immediately
		_ = repoRedis.UserSettings.SetObject(ctx, int64(30), user_settings_models.UserSettingsPayload{
			ID:     1,
			UserID: 30,
			Privacy: user_settings_models.PrivacySettings{
				ConversationPermission: 2,
				AllowMentions:          1,
			},
		})

		mSrv.setDocs("auth.users", bson.M{
			"id":         int64(30),
			"username":   "mongo_user",
			"first_name": "Mongo",
			"grade":      int32(2),
		})

		got, err := GetUserLite(ctx, 30)
		if err != nil {
			t.Fatalf("GetUserLite(30) from L2 error: %v", err)
		}
		if got.ID != 30 || got.Username != "mongo_user" || got.ConversationPermission != 2 || got.AllowMentions != 1 {
			t.Errorf("GetUserLite(30) from L2 = %+v", got)
		}

		// Verify L1 was rehydrated
		var cached lite_models.UserLiteRequest
		if err := repoRedis.UsersLite.GetObject(ctx, int64(30), &cached); err != nil || cached.Username != "mongo_user" {
			t.Errorf("expected L1 UsersLite to be rehydrated with mongo_user, got (%+v, %v)", cached, err)
		}
	})

	t.Run("GetUserLite L1 & L2 miss -> L3 Postgres hit promotes to L1 and L2", func(t *testing.T) {
		setupTestRedis(t)
		mSrv := setupTestMongo(t)
		mSrv.setFailCollection("auth.users", true) // force L2 error -> L3 fallback

		_ = repoRedis.UserSettings.SetObject(ctx, int64(40), user_settings_models.UserSettingsPayload{
			ID:     2,
			UserID: 40,
			Privacy: user_settings_models.PrivacySettings{
				HideConnections: 3,
			},
		})

		now := time.Now().UTC()
		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{
				cols: []string{
					"id", "username", "email", "email_verified", "phone", "phone_verified",
					"password_hash", "first_name", "last_name", "birthdate", "sex", "bio",
					"profile_picture_id", "grade", "location", "school", "work", "badges",
					"desactivated", "banned", "ban_reason", "ban_expires_at", "created_at", "updated_at",
				},
				rows: [][]driver.Value{
					{
						int64(40), "pg_user", "pg@test.com", true, "0600000000", true,
						"hash", "Paul", "Martin", "2000-01-01T00:00:00Z", int64(1), "Bio text",
						int64(99), int64(3), "Paris", "42", "Dev", nil,
						false, false, nil, nil, now, now,
					},
				},
			}
		})

		got, err := GetUserLite(ctx, 40)
		if err != nil {
			t.Fatalf("GetUserLite(40) from L3 error: %v", err)
		}
		if got.ID != 40 || got.Username != "pg_user" || got.HideConnections != 3 {
			t.Errorf("GetUserLite(40) from L3 = %+v", got)
		}
		time.Sleep(15 * time.Millisecond)
	})

	t.Run("GetUserLite returns 404 when L1, L2, and L3 all fail", func(t *testing.T) {
		setupTestRedis(t)
		mSrv := setupTestMongo(t)
		mSrv.setFailCollection("auth.users", true)
		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{err: errors.New("pg down")}
		})

		_, err := GetUserLite(ctx, 404)
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusNotFound {
			t.Errorf("GetUserLite(404) error = %v, want 404 NotFound", err)
		}
	})
}
