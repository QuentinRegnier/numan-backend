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

func TestFuncArchiveClosedReport(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		err := FuncArchiveClosedReport(ctx, 101, -1, "Closed by moderator")
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("archives_report_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "moderation.func_archive_closed_report")
			require.Len(t, args, 3)
			assert.Equal(t, int64(101), args[0].Value)
			assert.Equal(t, int64(-1), args[1].Value)
			assert.Equal(t, "Blâme confirmé", args[2].Value)
			return test.NewMockRows([]string{"archived"}, [][]driver.Value{{true}}), nil
		}, nil)

		err := FuncArchiveClosedReport(ctx, 101, -1, "Blâme confirmé")
		require.NoError(t, err)
	})

	t.Run("handles_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		}, nil)

		err := FuncArchiveClosedReport(ctx, 101, -1, "Blâme confirmé")
		test.AssertDatabaseError_Postgres(t, err)
	})
}
