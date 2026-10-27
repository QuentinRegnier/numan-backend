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

func TestFuncLoadUser(t *testing.T) {
	ctx := context.Background()

	t.Run("handles_uninitialized_database", func(t *testing.T) {
		orig := infradb.PostgresDB
		infradb.PostgresDB = nil
		defer func() { infradb.PostgresDB = orig }()

		u, err := FuncLoadUser(ctx, 1, "", "", "")
		assert.Empty(t, u.ID)
		test.AssertDatabaseError_Postgres(t, err)
	})

	cols := []string{
		"id", "username", "email", "email_verified", "phone", "phone_verified",
		"password_hash", "first_name", "last_name", "birthdate", "sex", "bio",
		"profile_picture_id", "grade", "location", "school", "work", "badges",
		"desactivated", "banned", "ban_reason", "ban_expires_at", "created_at", "updated_at",
		"two_factor_enabled", "two_factor_method", "totp_secret", "trust_score", "last_infraction_at",
	}
	now := time.Now()

	t.Run("returns_user_successfully", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assert.Contains(t, query, "auth.func_load_user")
			require.Len(t, args, 4)
			assert.Equal(t, int64(1), args[0].Value)
			assert.Equal(t, "alice", args[1].Value)
			return test.NewMockRows(cols, [][]driver.Value{
				{
					int64(1), "alice", "alice@example.com", true, "+33123456789", true,
					"hash123", "Alice", "Dupont", "2000-01-01T00:00:00Z", 1, "Hello world",
					int64(50), 1, "Paris", "University", "Company", "{}",
					false, false, nil, nil, now, now,
					false, 0, nil, float64(0.65), int64(0),
				},
			}), nil
		}, nil)

		u, err := FuncLoadUser(ctx, 1, "alice", "", "")
		require.NoError(t, err)
		assert.Equal(t, int64(1), u.ID)
		assert.Equal(t, "alice", u.Username)
		assert.Equal(t, "alice@example.com", u.Email)
		assert.Equal(t, "Alice", u.FirstName)
		assert.Equal(t, "Paris", u.Location)
		assert.Equal(t, 0.65, u.TrustScore)
	})

	t.Run("handles_user_not_found_returning_empty_payload_and_nil_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return test.NewMockRows(cols, [][]driver.Value{}), nil
		}, nil)

		u, err := FuncLoadUser(ctx, 999, "", "", "")
		require.NoError(t, err)
		assert.Empty(t, u.ID)
	})

	t.Run("handles_database_query_error", func(t *testing.T) {
		test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("db error")
		}, nil)

		u, err := FuncLoadUser(ctx, 1, "", "", "")
		assert.Empty(t, u.ID)
		test.AssertDatabaseError_Postgres(t, err)
	})
}
