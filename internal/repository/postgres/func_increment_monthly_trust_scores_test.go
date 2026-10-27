package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncIncrementMonthlyTrustScores(t *testing.T) {
	ctx := context.Background()
	cols := []string{"user_id", "new_trust_score"}

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		rewards, err := FuncIncrementMonthlyTrustScores(ctx)
		assert.Nil(t, rewards)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("returns_monthly_rewards_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_increment_monthly_trust_scores")
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1), float64(0.42)},
				{int64(2), float64(0.82)},
			}), nil
		}, nil)

		rewards, err := FuncIncrementMonthlyTrustScores(ctx)
		require.NoError(t, err)
		require.Len(t, rewards, 2)
		assert.Equal(t, int64(1), rewards[0].UserID)
		assert.Equal(t, 0.42, rewards[0].NewTrustScore)
		assert.Equal(t, int64(2), rewards[1].UserID)
		assert.Equal(t, 0.82, rewards[1].NewTrustScore)
	})

	t.Run("handles_empty_rewards", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		rewards, err := FuncIncrementMonthlyTrustScores(ctx)
		require.NoError(t, err)
		assert.NotNil(t, rewards)
		assert.Empty(t, rewards)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db query failure")
		}, nil)

		rewards, err := FuncIncrementMonthlyTrustScores(ctx)
		assert.Nil(t, rewards)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(1), float64(0.42)},
			}, errors.New("stream interrupted")), nil
		}, nil)

		rewards, err := FuncIncrementMonthlyTrustScores(ctx)
		assert.Nil(t, rewards)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
