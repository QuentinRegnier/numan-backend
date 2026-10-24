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

func TestFuncLoadActiveConversations(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		convs, err := FuncLoadActiveConversations(ctx)
		assert.Nil(t, convs)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{"id", "type", "title", "description", "avatar_id", "last_message_id", "settings", "external_link"}

	t.Run("returns_active_conversations_successfully", func(t *testing.T) {
		settingsJSON := `{"write_permission":1,"join_approval_required":true}`
		linksJSON := `{"title":"Discord","url":"https://discord.gg/numan"}`
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "messaging.func_load_active_conversations")
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1), 1, "Public Conv", "Discussion", int64(10), int64(100), settingsJSON, linksJSON},
				{int64(2), 0, nil, nil, nil, nil, nil, nil},
			}), nil
		}, nil)

		convs, err := FuncLoadActiveConversations(ctx)
		require.NoError(t, err)
		require.Len(t, convs, 2)
		assert.Equal(t, int64(1), convs[0].ID)
		assert.Equal(t, "Public Conv", convs[0].Title)
		assert.Equal(t, "Discussion", convs[0].Description)
		assert.Equal(t, int64(10), convs[0].AvatarID)
		assert.Equal(t, int64(100), convs[0].LastMessageID)
		assert.Equal(t, 1, convs[0].Settings.WritePermission)
		assert.True(t, convs[0].Settings.JoinApprovalRequired)
		assert.Equal(t, "Discord", convs[0].ExternalLink.Title)
		assert.Equal(t, "https://discord.gg/numan", convs[0].ExternalLink.URL)

		assert.Equal(t, int64(2), convs[1].ID)
		assert.Equal(t, "", convs[1].Title)
	})

	t.Run("handles_invalid_settings_json", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1), 1, "Title", nil, nil, nil, "invalid-json", nil},
			}), nil
		}, nil)

		convs, err := FuncLoadActiveConversations(ctx)
		assert.Nil(t, convs)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_invalid_external_link_json", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{
				{int64(1), 1, "Title", nil, nil, nil, nil, "invalid-links-json"},
			}), nil
		}, nil)

		convs, err := FuncLoadActiveConversations(ctx)
		assert.Nil(t, convs)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_empty_conversations", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		convs, err := FuncLoadActiveConversations(ctx)
		require.NoError(t, err)
		assert.NotNil(t, convs)
		assert.Empty(t, convs)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db failure")
		}, nil)

		convs, err := FuncLoadActiveConversations(ctx)
		assert.Nil(t, convs)
		test.AssertDatabaseError_Postgres(t, err)
	})

	t.Run("handles_row_iteration_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRowsWithErr(cols, [][]driver.Value{{int64(1), 0, nil, nil, nil, nil, nil, nil}}, errors.New("iter err")), nil
		}, nil)

		convs, err := FuncLoadActiveConversations(ctx)
		assert.Nil(t, convs)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
