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

func TestFuncApplyTrustScoreDelta(t *testing.T) {
	ctx := context.Background()
	cols := []string{"new_score", "is_banned"}

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		res, err := FuncApplyTrustScoreDelta(ctx, 1, 0.05, false, false, "")
		assert.Empty(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("applies_delta_bonus_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_apply_trust_score_delta")
			require.Len(t, args, 5)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, float64(0.05), args[1].Value)
			assert.Equal(t, false, args[2].Value)
			assert.Equal(t, false, args[3].Value)
			assert.Nil(t, args[4].Value)
			return test.NewMockRows(cols, [][]driver.Value{{float64(0.45), false}}), nil
		}, nil)

		res, err := FuncApplyTrustScoreDelta(ctx, 10, 0.05, false, false, "")
		require.NoError(t, err)
		assert.Equal(t, 0.45, res.NewScore)
		assert.False(t, res.IsBanned)
	})

	t.Run("applies_major_ban_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_apply_trust_score_delta")
			require.Len(t, args, 5)
			assert.Equal(t, int64(20), args[0].Value)
			assert.Equal(t, float64(-1.0), args[1].Value)
			assert.Equal(t, true, args[2].Value)
			assert.Equal(t, true, args[3].Value)
			assert.Equal(t, "Contenu illicite", args[4].Value)
			return test.NewMockRows(cols, [][]driver.Value{{float64(0.00), true}}), nil
		}, nil)

		res, err := FuncApplyTrustScoreDelta(ctx, 20, -1.0, true, true, "Contenu illicite")
		require.NoError(t, err)
		assert.Equal(t, 0.00, res.NewScore)
		assert.True(t, res.IsBanned)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("connection reset by peer")
		}, nil)

		res, err := FuncApplyTrustScoreDelta(ctx, 10, -0.30, true, false, "Infraction mineure")
		assert.Empty(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
