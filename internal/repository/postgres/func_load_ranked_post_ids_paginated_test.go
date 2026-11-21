package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	infraPg "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncLoadRankedPostIDsPaginated(t *testing.T) {
	ctx := context.Background()

	t.Run("success without lang filter and with lang filter", func(t *testing.T) {
		var capturedQuery string
		var capturedArgs []driver.NamedValue
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			capturedQuery = query
			capturedArgs = args
			return test.NewMockRows([]string{"id"}, [][]driver.Value{{int64(101)}, {int64(102)}}), nil
		}, nil)

		ids, err := FuncLoadRankedPostIDsPaginated(ctx, variables.OrderModeLikes, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []int64{101, 102}, ids)
		assert.True(t, strings.Contains(capturedQuery, "func_load_ranked_post_ids_paginated"))
		require.Len(t, capturedArgs, 3)

		idsLang, err := FuncLoadRankedPostIDsPaginated(ctx, variables.OrderModeViews, 5, 10, variables.LangEN)
		require.NoError(t, err)
		assert.Equal(t, []int64{101, 102}, idsLang)
		require.Len(t, capturedArgs, 4)
		assert.Equal(t, int64(variables.LangEN), capturedArgs[3].Value)
	})

	t.Run("nil PostgresDB and query error", func(t *testing.T) {
		prevDB := infraPg.PostgresDB
		infraPg.PostgresDB = nil
		_, err := FuncLoadRankedPostIDsPaginated(ctx, variables.OrderModeRecent, 0, 10)
		infraPg.PostgresDB = prevDB
		require.Error(t, err)

		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("pg err")
		}, nil)
		_, err = FuncLoadRankedPostIDsPaginated(ctx, variables.OrderModeRecent, 0, 10)
		require.Error(t, err)
	})
}
