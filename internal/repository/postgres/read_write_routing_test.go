package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresReadWriteRouting_PureReadsHitReplicaOnly(t *testing.T) {
	ctx := context.Background()
	var writeHits, readHits atomic.Int32

	test.SetupTestPostgresReadWrite(
		t,
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			writeHits.Add(1)
			return nil, errors.New("pure read must not hit Primary WriteDB")
		},
		func(query string, args []driver.NamedValue) (driver.Result, error) {
			writeHits.Add(1)
			return nil, errors.New("pure read must not exec on Primary WriteDB")
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			readHits.Add(1)
			return test.NewMockRows([]string{"tag"}, [][]driver.Value{{"golang"}, {"k8s"}}), nil
		},
		nil,
	)

	tags, err := FuncLoadAllTags(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"golang", "k8s"}, tags)
	assert.Equal(t, int32(0), writeHits.Load(), "Primary WriteDB ne doit pas être sollicité pour une lecture pure")
	assert.Equal(t, int32(1), readHits.Load(), "Read Replica ReadDB doit servir les lectures pures")
}

func TestPostgresReadWriteRouting_CriticalAuthReadsAndWritesHitPrimaryOnly(t *testing.T) {
	ctx := context.Background()

	t.Run("FuncCheckUnique routes to Primary WriteDB (0ms lag)", func(t *testing.T) {
		var writeHits, readHits atomic.Int32
		test.SetupTestPostgresReadWrite(
			t,
			func(query string, args []driver.NamedValue) (driver.Rows, error) {
				writeHits.Add(1)
				return test.NewMockRows([]string{"func_check_unique"}, [][]driver.Value{{true}}), nil
			},
			nil,
			func(query string, args []driver.NamedValue) (driver.Rows, error) {
				readHits.Add(1)
				return nil, errors.New("FuncCheckUnique must not hit Read Replica")
			},
			nil,
		)

		exists, err := FuncCheckUnique(ctx, redis.EntityUser, "username", "alice")
		require.NoError(t, err)
		assert.True(t, exists)
		assert.Equal(t, int32(1), writeHits.Load())
		assert.Equal(t, int32(0), readHits.Load())
	})

	t.Run("FuncLoadSession routes to Primary WriteDB (0ms lag)", func(t *testing.T) {
		var writeHits, readHits atomic.Int32
		test.SetupTestPostgresReadWrite(
			t,
			func(query string, args []driver.NamedValue) (driver.Rows, error) {
				writeHits.Add(1)
				return test.NewMockRows([]string{"id", "user_id", "master_token", "firebase_installation_id", "device_info", "ip_history", "created_at", "expires_at"}, nil), nil
			},
			nil,
			func(query string, args []driver.NamedValue) (driver.Rows, error) {
				readHits.Add(1)
				return nil, errors.New("FuncLoadSession must not hit Read Replica")
			},
			nil,
		)

		sess, err := FuncLoadSession(ctx, 42, -1, "", "")
		require.NoError(t, err)
		assert.Zero(t, sess.ID)
		assert.Equal(t, int32(1), writeHits.Load())
		assert.Equal(t, int32(0), readHits.Load())
	})

	t.Run("FuncGetPasskeyByCredentialID routes to Primary WriteDB (0ms lag)", func(t *testing.T) {
		var writeHits, readHits atomic.Int32
		test.SetupTestPostgresReadWrite(
			t,
			func(query string, args []driver.NamedValue) (driver.Rows, error) {
				writeHits.Add(1)
				return test.NewMockRows([]string{"id", "user_id", "credential_id", "public_key", "attestation_type", "aaguid", "sign_count", "device_name", "transports", "created_at", "updated_at"}, nil), nil
			},
			nil,
			func(query string, args []driver.NamedValue) (driver.Rows, error) {
				readHits.Add(1)
				return nil, errors.New("FuncGetPasskeyByCredentialID must not hit Read Replica")
			},
			nil,
		)

		pk, err := FuncGetPasskeyByCredentialID(ctx, "cred-123")
		require.NoError(t, err)
		assert.Zero(t, pk.ID)
		assert.Equal(t, int32(1), writeHits.Load())
		assert.Equal(t, int32(0), readHits.Load())
	})

	t.Run("Write repository FuncDeleteOrphanLikes routes to Primary WriteDB", func(t *testing.T) {
		var writeHits, readHits atomic.Int32
		test.SetupTestPostgresReadWrite(
			t,
			func(query string, args []driver.NamedValue) (driver.Rows, error) {
				writeHits.Add(1)
				return test.NewMockRows([]string{"deleted"}, [][]driver.Value{{int64(3)}}), nil
			},
			func(query string, args []driver.NamedValue) (driver.Result, error) {
				writeHits.Add(1)
				return driver.RowsAffected(3), nil
			},
			func(query string, args []driver.NamedValue) (driver.Rows, error) {
				readHits.Add(1)
				return nil, errors.New("write mutation must not hit Read Replica")
			},
			func(query string, args []driver.NamedValue) (driver.Result, error) {
				readHits.Add(1)
				return nil, errors.New("write mutation must not exec on Read Replica")
			},
		)

		deleted, err := FuncDeleteOrphanLikes(ctx)
		require.NoError(t, err)
		assert.NotNil(t, deleted)
		assert.Equal(t, int32(1), writeHits.Load())
		assert.Equal(t, int32(0), readHits.Load())
	})
}
