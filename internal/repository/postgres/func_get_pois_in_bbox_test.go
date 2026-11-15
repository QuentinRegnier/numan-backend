package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncGetPOIsInBBox(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		res, err := FuncGetPOIsInBBox(ctx, 48.80, 2.25, 48.90, 2.40, 100)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{
		"id", "type", "title", "description", "latitude", "longitude", "address",
		"phone", "website", "rating", "comment_count", "media_ids", "google_maps_link", "apple_maps_link", "visibility", "created_at", "updated_at",
	}
	now := domain.NowMillis()

	t.Run("returns_pois_in_bbox_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_get_pois_in_bbox")
			require.Len(t, args, 5)
			assert.InDelta(t, 48.80, args[0].Value, 1e-6)
			assert.InDelta(t, 2.25, args[1].Value, 1e-6)
			assert.InDelta(t, 48.90, args[2].Value, 1e-6)
			assert.InDelta(t, 2.40, args[3].Value, 1e-6)
			return test.NewMockRows(cols, [][]driver.Value{
				{
					int64(8001), int64(1), "Arc de Triomphe", "Place Charles de Gaulle", 48.873792, 2.295028, "Pl. Charles de Gaulle, 75008 Paris",
					nil, nil, 4.7, int64(310), "{}", nil, nil, int64(0), now, now,
				},
			}), nil
		}, nil)

		pois, err := FuncGetPOIsInBBox(ctx, 48.80, 2.25, 48.90, 2.40, 0)
		require.NoError(t, err)
		require.Len(t, pois, 1)
		assert.Equal(t, int64(8001), pois[0].ID)
		assert.Equal(t, "Arc de Triomphe", pois[0].Title)
		assert.Empty(t, pois[0].Phone)
		assert.Empty(t, pois[0].Website)
		assert.Empty(t, pois[0].GoogleMapsLink)
		assert.Empty(t, pois[0].AppleMapsLink)
		assert.Empty(t, pois[0].MediaIDs)
	})

	t.Run("handles_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query timeout")
		}, nil)

		pois, err := FuncGetPOIsInBBox(ctx, 48.80, 2.25, 48.90, 2.40, 50)
		assert.Nil(t, pois)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
