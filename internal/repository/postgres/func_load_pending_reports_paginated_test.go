package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncLoadPendingReportsPaginated(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UnixMilli()
	cols := []string{
		"id", "reporter_id", "report_origin", "target_type", "target_ids",
		"target_owner_id", "author_trust_score", "ia_score_max", "accumulated_report_score", "report_count",
		"category", "reason", "rationale", "state", "importance", "created_at", "updated_at",
	}

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		reports, err := FuncLoadPendingReportsPaginated(ctx, 10, 0)
		assert.Nil(t, reports)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("returns_pending_reports_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "moderation.func_load_pending_reports_paginated")
			require.Len(t, args, 4)
			assert.Equal(t, int64(10), args[0].Value)
			assert.Equal(t, int64(0), args[1].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(501), int64(0), 1, 0, "{100}", int64(42), float64(0.20), float64(0.85), float64(0.0), 1, 10, "Auto AI NSFW", "", 0, float64(2.24), now, now},
				{int64(502), int64(14), 0, 3, "{200,201}", int64(55), float64(0.40), float64(0.60), float64(1.35), 2, 2, "harassment", "", 0, float64(2.60), now, now},
			}), nil
		}, nil)

		reports, err := FuncLoadPendingReportsPaginated(ctx, 10, 0)
		require.NoError(t, err)
		require.Len(t, reports, 2)
		assert.Equal(t, int64(501), reports[0].ID)
		assert.Equal(t, 1, reports[0].ReportOrigin)
		assert.Equal(t, "auto_ai", reports[0].ReportOriginLabel)
		assert.Equal(t, 0.85, reports[0].IAScoreMax)
		assert.Equal(t, 2.24, reports[0].Importance)
		assert.Equal(t, int64(502), reports[1].ID)
		assert.Equal(t, "user", reports[1].ReportOriginLabel)
		assert.Equal(t, []int64{200, 201}, reports[1].TargetIDs)
	})

	t.Run("handles_empty_results", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		reports, err := FuncLoadPendingReportsPaginated(ctx, 10, 0)
		require.NoError(t, err)
		assert.NotNil(t, reports)
		assert.Empty(t, reports)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db query error")
		}, nil)

		reports, err := FuncLoadPendingReportsPaginated(ctx, 10, 0)
		assert.Nil(t, reports)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(501), int64(12), 0, 0, "{100}", int64(42), float64(0.50), float64(0.10), float64(0.5), 1, 7, "spam", "", 0, float64(150.5), now, now},
			}, errors.New("stream interrupted")), nil
		}, nil)

		reports, err := FuncLoadPendingReportsPaginated(ctx, 10, 0)
		assert.Nil(t, reports)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
