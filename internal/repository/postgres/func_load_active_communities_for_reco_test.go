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

func TestFuncLoadActiveCommunitiesForReco(t *testing.T) {
	ctx := context.Background()
	cols := []string{"id", "title", "description", "avatar_id", "lang", "is_partner", "pin_slot", "settings", "created_at", "member_count"}

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		res, err := FuncLoadActiveCommunitiesForReco(ctx)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("returns_active_communities_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_load_active_communities_for_reco")
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(10), "Go FR", "Communauté Go", int64(55), "fr", true, int64(1), `{"write_permission":0,"join_approval_required":true}`, int64(1700000000000), int64(42)},
				{int64(11), "Rust EN", "Rust chat", int64(0), "en", false, nil, `{}`, int64(1700000001000), int64(12)},
			}), nil
		}, nil)

		res, err := FuncLoadActiveCommunitiesForReco(ctx)
		require.NoError(t, err)
		require.Len(t, res, 2)

		assert.Equal(t, int64(10), res[0].ID)
		assert.Equal(t, "Go FR", res[0].Title)
		assert.Equal(t, "Communauté Go", res[0].Description)
		assert.Equal(t, int64(55), res[0].AvatarID)
		assert.Equal(t, "fr", res[0].Lang)
		assert.True(t, res[0].IsPartner)
		assert.Equal(t, 1, res[0].PinSlot)
		assert.Equal(t, 0, res[0].Settings.WritePermission)
		assert.True(t, res[0].Settings.JoinApprovalRequired)
		assert.Equal(t, 42, res[0].MemberCount)

		assert.Equal(t, int64(11), res[1].ID)
		assert.Equal(t, "en", res[1].Lang)
		assert.False(t, res[1].IsPartner)
		assert.Equal(t, 0, res[1].PinSlot)
	})

	t.Run("handles_invalid_settings_json", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(10), "Go FR", "", int64(0), "fr", false, nil, "bad-json", int64(1700000000000), int64(1)},
			}), nil
		}, nil)

		res, err := FuncLoadActiveCommunitiesForReco(ctx)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db failure")
		}, nil)

		res, err := FuncLoadActiveCommunitiesForReco(ctx)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{
				{int64(10), "Go FR", "", int64(0), "fr", false, nil, `{}`, int64(1700000000000), int64(1)},
			}, errors.New("iter err")), nil
		}, nil)

		res, err := FuncLoadActiveCommunitiesForReco(ctx)
		assert.Nil(t, res)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
