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

func TestFuncSetCommunityPartner(t *testing.T) {
	ctx := context.Background()
	cols := []string{"status_code", "conv_lang", "is_partner"}

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		_, err := FuncSetCommunityPartner(ctx, 10, true)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("sets_partner_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_set_community_partner")
			require.Len(t, args, 2)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, true, args[1].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{"OK", "fr", true},
			}), nil
		}, nil)

		res, err := FuncSetCommunityPartner(ctx, 10, true)
		require.NoError(t, err)
		assert.Equal(t, "OK", res.StatusCode)
		assert.Equal(t, "fr", res.ConvLang)
		assert.True(t, res.IsPartner)
	})

	t.Run("handles_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		_, err := FuncSetCommunityPartner(ctx, 10, false)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
