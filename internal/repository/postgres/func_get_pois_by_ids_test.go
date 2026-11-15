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

func TestFuncGetPOIsByIDs(t *testing.T) {
	ctx := context.Background()

	t.Run("empty_ids_returns_empty_slice_immediately", func(t *testing.T) {
		res, err := FuncGetPOIsByIDs(ctx, nil)
		require.NoError(t, err)
		assert.Empty(t, res)
	})

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		res, err := FuncGetPOIsByIDs(ctx, []int64{101})
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{
		"id", "type", "title", "description", "latitude", "longitude", "address",
		"phone", "website", "rating", "comment_count", "media_ids", "google_maps_link", "apple_maps_link", "visibility", "created_at", "updated_at",
	}
	now := domain.NowMillis()

	t.Run("returns_pois_with_persisted_links", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_get_pois_by_ids")
			require.Len(t, args, 1)
			return test.NewMockRows(cols, [][]driver.Value{
				{
					int64(7001), int64(2), "Musée d'Orsay", "Musée national", 48.859961, 2.326561, "1 Rue de la Légion d'Honneur",
					"+33140494814", "https://www.musee-orsay.fr", 4.8, int64(512), "{901,902}", "https://maps.app.goo.gl/orsay", "https://maps.apple.com/?place-id=orsay", int64(0), now, now,
				},
			}), nil
		}, nil)

		pois, err := FuncGetPOIsByIDs(ctx, []int64{7001})
		require.NoError(t, err)
		require.Len(t, pois, 1)
		assert.Equal(t, int64(7001), pois[0].ID)
		assert.Equal(t, "Musée d'Orsay", pois[0].Title)
		assert.Equal(t, []int64{901, 902}, pois[0].MediaIDs)
		assert.Equal(t, "https://maps.app.goo.gl/orsay", pois[0].GoogleMapsLink)
		assert.Equal(t, "https://maps.apple.com/?place-id=orsay", pois[0].AppleMapsLink)
	})

	t.Run("handles_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("connection lost")
		}, nil)

		pois, err := FuncGetPOIsByIDs(ctx, []int64{7001})
		assert.Nil(t, pois)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
