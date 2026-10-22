package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncCheckUnique(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_unsupported_entity", func(t *testing.T) {
		exists, err := FuncCheckUnique(ctx, redis.EntityType("unknown"), "field", "val")
		assert.False(t, exists)
		assertDatabaseError(t, err)
	})

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		exists, err := FuncCheckUnique(ctx, redis.EntityUser, "username", "alice")
		assert.False(t, exists)
		assertDatabaseError(t, err)
	})

	t.Run("returns_true_when_record_exists", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "views.func_check_unique")
			require.Len(t, args, 4)
			assert.Equal(t, "auth", args[0].Value)
			assert.Equal(t, "users", args[1].Value)
			assert.Equal(t, "username", args[2].Value)
			assert.Equal(t, "alice", args[3].Value)
			return newMockRows([]string{"exists"}, [][]driver.Value{{true}}), nil
		}, nil)

		exists, err := FuncCheckUnique(ctx, redis.EntityUser, "username", "alice")
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("returns_false_when_record_does_not_exist", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return newMockRows([]string{"exists"}, [][]driver.Value{{false}}), nil
		}, nil)

		exists, err := FuncCheckUnique(ctx, redis.EntityUser, "username", "bob")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("handles_query_error", func(t *testing.T) {
		setupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		}, nil)

		exists, err := FuncCheckUnique(ctx, redis.EntityUser, "username", "alice")
		assert.False(t, exists)
		assertDatabaseError(t, err)
	})
}
