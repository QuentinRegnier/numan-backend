package cache_service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVariablesCacheService(t *testing.T) {
	ctx := context.Background()

	t.Run("Full lifecycle: Bind, Ready, Load, Save, Version, Publish, Subscribe, ApplyAndBroadcast", func(t *testing.T) {
		_ = test.SetupTestRedis(t)

		BindVariablesCacheHooks()
		assert.True(t, IsVariablesCacheReady())

		// Initial remote version is 0 (goredis.Nil)
		ver, err := GetDynamicVariablesRemoteVersion(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), ver)

		// Subscribe before broadcasting
		ch, cancel := SubscribeDynamicVariablesSync(ctx)
		defer cancel()
		time.Sleep(20 * time.Millisecond)

		patch := &variables.PreparedPatch{
			ToSet:    map[string]string{"max_upload": "50"},
			ToDelete: []string{"old_key"},
		}
		newVer, err := ApplyAndBroadcastDynamicVariablesPatch(ctx, patch)
		require.NoError(t, err)
		assert.Equal(t, int64(1), newVer)

		ver, err = GetDynamicVariablesRemoteVersion(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), ver)

		overrides, loadedVer, err := LoadDynamicVariablesFromCache(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), loadedVer)
		assert.Equal(t, "50", overrides["max_upload"])

		select {
		case msg := <-ch:
			assert.NotEmpty(t, msg)
		case <-time.After(500 * time.Millisecond):
			t.Fatal("expected sync event from SubscribeDynamicVariablesSync")
		}
	})

	t.Run("Error Paths", func(t *testing.T) {
		srv := test.SetupTestRedis(t)
		var appErr *numan_error.AppError

		// EVAL failures on Load and Save
		srv.SetFailCmd("EVAL", errors.New("eval err"))
		_, _, err := LoadDynamicVariablesFromCache(ctx)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)

		_, err = SaveDynamicVariablesPatchInCache(ctx, map[string]string{"k": "v"}, nil)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)

		_, err = ApplyAndBroadcastDynamicVariablesPatch(ctx, &variables.PreparedPatch{ToSet: map[string]string{"k": "v"}})
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("EVAL", nil)

		// GET failure on GetDynamicVariablesRemoteVersion
		srv.SetFailCmd("GET", errors.New("get err"))
		_, err = GetDynamicVariablesRemoteVersion(ctx)
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
		srv.SetFailCmd("GET", nil)

		// Publish failure (SET or PUBLISH)
		srv.SetFailCmd("SET", errors.New("set err"))
		err = PublishDynamicVariablesSync(ctx, VariablesSyncEvent{Version: 2, UpdatedAt: 100})
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)

		// ApplyAndBroadcast logs warning on publish failure but still returns newVersion
		v, err := ApplyAndBroadcastDynamicVariablesPatch(ctx, &variables.PreparedPatch{ToSet: map[string]string{"k": "v"}})
		require.NoError(t, err)
		assert.Equal(t, int64(1), v)
		srv.SetFailCmd("SET", nil)

		// Redis not ready in ApplyAndBroadcastDynamicVariablesPatch
		prevRdb := infraRedis.Rdb
		infraRedis.Rdb = nil
		_, err = ApplyAndBroadcastDynamicVariablesPatch(ctx, &variables.PreparedPatch{})
		infraRedis.Rdb = prevRdb
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, numan_error.CodeCacheError, appErr.Code)
	})
}
