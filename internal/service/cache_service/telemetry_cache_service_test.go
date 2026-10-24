package cache_service

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoMongo "github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestTelemetryCacheService(t *testing.T) {
	ctx := context.Background()

	t.Run("SetTelemetryProfile and GetTelemetryProfile L1 Hit", func(t *testing.T) {
		srv := test.SetupTestRedis(t)

		expected := lite_models.TelemetryProfileLite{
			Vector:          []float32{0.1, 0.2},
			TopTags:         []string{"go", "redis"},
			ConfidenceScore: 0.9,
			TimestampMs:     12345,
		}
		require.NoError(t, SetTelemetryProfile(ctx, 1, expected))

		got, err := GetTelemetryProfile(ctx, 1)
		require.NoError(t, err)
		assert.Equal(t, expected, got)

		srv.SetFailCmd("SET", errors.New("set err"))
		var appErr *numan_error.AppError
		require.ErrorAs(t, SetTelemetryProfile(ctx, 1, expected), &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
	})

	t.Run("GetTelemetryProfile Cascade L2 Mongo -> L3 Postgres -> Complete Miss", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
		restore := test.ResetCollections()
		defer restore()

		// 1. L2 Hit
		mt.Run("L2 Hit", func(mt *mtest.T) {
			repoMongo.UserSettings = test.NewMockCollection(mt, "auth.user_settings", nil)
			mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.user_settings", mtest.FirstBatch, bson.D{
				{Key: "id", Value: int64(1)},
				{Key: "user_id", Value: int64(10)},
				{Key: "telemetry_vector", Value: bson.A{0.5, 0.25}},
				{Key: "telemetry_tags", Value: bson.A{"ai"}},
				{Key: "telemetry_timestamp", Value: int64(999)},
			}))

			got, err := GetTelemetryProfile(ctx, 10)
			require.NoError(t, err)
			assert.Equal(t, []float32{0.5, 0.25}, got.Vector)
			assert.Equal(t, []string{"ai"}, got.TopTags)
			time.Sleep(20 * time.Millisecond)
		})

		// 2. L2 Error -> L3 Hit (with async promotion failure logged)
		mt.Run("L3 Hit", func(mt *mtest.T) {
			repoMongo.UserSettings = test.NewMockCollection(mt, "auth.user_settings", nil)
			mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 1, Message: "mongo err"}))

			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return test.NewMockRows(
					[]string{"id", "user_id", "privacy", "notifications", "display_and_content", "telemetry_vector", "telemetry_tags", "telemetry_timestamp"},
					[][]driver.Value{{int64(2), int64(20), []byte(`{}`), []byte(`{}`), []byte(`{}`), `{0.7,0.8}`, `{cloud}`, int64(777)}},
				), nil
			}, nil)

			srv.SetFailCmd("SET", errors.New("set warn"))
			got, err := GetTelemetryProfile(ctx, 20)
			require.NoError(t, err)
			assert.Equal(t, []float32{0.7, 0.8}, got.Vector)
			assert.Equal(t, []string{"cloud"}, got.TopTags)
			time.Sleep(20 * time.Millisecond)
			srv.SetFailCmd("SET", nil)
		})

		// 3. Complete Miss -> CodeTelemetrySyncRequired
		mt.Run("Complete Miss", func(mt *mtest.T) {
			repoMongo.UserSettings = test.NewMockCollection(mt, "auth.user_settings", nil)
			mt.AddMockResponses(mtest.CreateCursorResponse(0, "numan_mongo.auth.user_settings", mtest.FirstBatch))

			test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
				return nil, errors.New("pg down")
			}, nil)

			_, err := GetTelemetryProfile(ctx, 404)
			var appErr *numan_error.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
			assert.Equal(t, numan_error.CodeTelemetrySyncRequired, appErr.Code)
		})
	})
}
