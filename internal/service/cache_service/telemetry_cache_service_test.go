package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/lib/pq"
	"go.mongodb.org/mongo-driver/bson"
)

func TestTelemetryCacheService(t *testing.T) {
	ctx := context.Background()

	t.Run("SetTelemetryProfile stores profile and GetTelemetryProfile returns L1 hit", func(t *testing.T) {
		setupTestRedis(t)
		want := lite_models.TelemetryProfileLite{
			Vector:          []float32{0.1, 0.2, 0.7},
			TopTags:         []string{"golang", "backend"},
			ConfidenceScore: 0.95,
			TimestampMs:     1700000000000,
		}

		if err := SetTelemetryProfile(ctx, 42, want); err != nil {
			t.Fatalf("SetTelemetryProfile() unexpected error: %v", err)
		}

		got, err := GetTelemetryProfile(ctx, 42)
		if err != nil {
			t.Fatalf("GetTelemetryProfile() L1 error: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("GetTelemetryProfile() L1 = %+v, want %+v", got, want)
		}
	})

	t.Run("L1 miss -> L2 MongoDB hit rehydrates L1 and returns profile", func(t *testing.T) {
		setupTestRedis(t)
		mSrv := setupTestMongo(t)
		mSrv.setDocs("auth.user_settings", bson.M{
			"id":                  int64(1),
			"user_id":             int64(55),
			"telemetry_vector":    bson.A{0.5, 0.5},
			"telemetry_tags":      bson.A{"music", "art"},
			"telemetry_timestamp": int64(1700000050000),
		})

		got, err := GetTelemetryProfile(ctx, 55)
		if err != nil {
			t.Fatalf("GetTelemetryProfile() L2 error: %v", err)
		}
		if !reflect.DeepEqual(got.TopTags, []string{"music", "art"}) || got.TimestampMs != 1700000050000 {
			t.Errorf("GetTelemetryProfile() L2 = %+v", got)
		}

		time.Sleep(20 * time.Millisecond)
		var l1Copy lite_models.TelemetryProfileLite
		if err := repoRedis.TelemetryProfiles.GetObject(ctx, int64(55), &l1Copy); err != nil {
			t.Errorf("expected L1 TelemetryProfiles to be rehydrated from L2, got error: %v", err)
		}
	})

	t.Run("L1 & L2 miss -> L3 Postgres hit rehydrates L1 and returns profile", func(t *testing.T) {
		setupTestRedis(t)
		mSrv := setupTestMongo(t)
		mSrv.setDocs("auth.user_settings") // empty L2

		vecVal, _ := pq.Array([]float32{0.25, 0.75}).Value()
		tagsVal, _ := pq.Array([]string{"cinema"}).Value()

		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{
				cols: []string{"id", "user_id", "privacy", "notifications", "display_and_content", "telemetry_vector", "telemetry_tags", "telemetry_timestamp"},
				rows: [][]driver.Value{
					{int64(9), int64(66), []byte(`{}`), []byte(`{}`), []byte(`{}`), vecVal, tagsVal, int64(1700000099000)},
				},
			}
		})

		got, err := GetTelemetryProfile(ctx, 66)
		if err != nil {
			t.Fatalf("GetTelemetryProfile() L3 error: %v", err)
		}
		if !reflect.DeepEqual(got.TopTags, []string{"cinema"}) || got.TimestampMs != 1700000099000 {
			t.Errorf("GetTelemetryProfile() L3 = %+v", got)
		}

		time.Sleep(20 * time.Millisecond)
		var l1Copy lite_models.TelemetryProfileLite
		if err := repoRedis.TelemetryProfiles.GetObject(ctx, int64(66), &l1Copy); err != nil {
			t.Errorf("expected L1 TelemetryProfiles to be rehydrated from L3, got error: %v", err)
		}
	})

	t.Run("complete L1/L2/L3 miss returns 404 TELEMETRY_SYNC_REQUIRED", func(t *testing.T) {
		setupTestRedis(t)
		mSrv := setupTestMongo(t)
		mSrv.setDocs("auth.user_settings")
		setupTestPostgres(t, func(query string, args []driver.NamedValue) fakeSQLResponse {
			return fakeSQLResponse{
				cols: []string{"id", "user_id", "privacy", "notifications", "display_and_content", "telemetry_vector", "telemetry_tags", "telemetry_timestamp"},
				rows: nil, // sql.ErrNoRows -> FuncLoadUserSettings returns ID=0, nil
			}
		})

		_, err := GetTelemetryProfile(ctx, 404)
		if err == nil {
			t.Fatal("GetTelemetryProfile(404) error = nil, want NotFound error")
		}
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusNotFound || appErr.Code != "TELEMETRY_SYNC_REQUIRED" {
			t.Errorf("GetTelemetryProfile(404) error = %+v, want 404 TELEMETRY_SYNC_REQUIRED", err)
		}
	})

	t.Run("SetTelemetryProfile returns internal error when Redis SET fails", func(t *testing.T) {
		srv := setupTestRedis(t)
		srv.setFailCmd("SET", errors.New("set error"))
		defer srv.setFailCmd("SET", nil)

		err := SetTelemetryProfile(ctx, 42, lite_models.TelemetryProfileLite{})
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("SetTelemetryProfile() error = %v, want 500 AppError", err)
		}
	})
}
