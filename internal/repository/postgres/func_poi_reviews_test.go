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

func TestFuncPOIReviews(t *testing.T) {
	ctx := context.Background()
	cols := []string{"id", "poi_id", "user_id", "rating", "content", "visibility", "created_at", "updated_at"}
	now := domain.NowMillis()

	t.Run("get_by_user_returns_found_and_not_found", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_get_poi_review_by_user")
			require.Len(t, args, 2)
			if args[1].Value.(int64) == 999 {
				return test.NewMockRows(cols, nil), nil
			}
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(8001), int64(5001), int64(1001), int64(5), "", int64(0), now, now},
			}), nil
		}, nil)

		r, found, err := FuncGetPOIReviewByUser(ctx, 5001, 1001)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, int64(8001), r.ID)
		assert.Equal(t, int8(5), r.Rating)
		assert.Equal(t, "", r.Content)

		_, foundMissing, errMissing := FuncGetPOIReviewByUser(ctx, 5001, 999)
		require.NoError(t, errMissing)
		assert.False(t, foundMissing)
	})

	t.Run("get_paginated_returns_text_reviews", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_get_poi_reviews_paginated")
			require.Len(t, args, 3)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(8002), int64(5001), int64(1002), int64(4), "Super spot", int64(0), now, now},
			}), nil
		}, nil)

		reviews, err := FuncGetPOIReviewsPaginated(ctx, 5001, 20, 0)
		require.NoError(t, err)
		require.Len(t, reviews, 1)
		assert.Equal(t, "Super spot", reviews[0].Content)
	})

	t.Run("get_by_ids_returns_reviews", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "content.func_get_poi_reviews_by_ids")
			require.Len(t, args, 1)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(8002), int64(5001), int64(1002), int64(4), "Super spot", int64(0), now, now},
			}), nil
		}, nil)

		reviews, err := FuncGetPOIReviewsByIDs(ctx, []int64{8002})
		require.NoError(t, err)
		require.Len(t, reviews, 1)
		assert.Equal(t, int64(8002), reviews[0].ID)
	})

	t.Run("handles_uninitialized_db_and_query_error", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		_, _, err := FuncGetPOIReviewByUser(ctx, 5001, 1001)
		test.AssertDatabaseError_Postgres(t, err)
		_, err = FuncGetPOIReviewsPaginated(ctx, 5001, 20, 0)
		test.AssertDatabaseError_Postgres(t, err)
		_, err = FuncGetPOIReviewsByIDs(ctx, []int64{8002})
		test.AssertDatabaseError_Postgres(t, err)
		infradb.PostgresDB = orig

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db failure")
		}, nil)
		_, err = FuncGetPOIReviewsPaginated(ctx, 5001, 20, 0)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
