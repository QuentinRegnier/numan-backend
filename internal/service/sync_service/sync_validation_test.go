package sync_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/sync_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/sync_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertSyncServiceBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestSyncServices_InvalidPayloads_NoRedisSideEffects(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	initialKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)

	_, err = sync_service.GetDeltas(ctx, 1, sync_models.GetDeltasInput{SinceMs: -1})
	assertSyncServiceBadRequest(t, err)

	_, err = sync_service.GetDeltas(ctx, 0, sync_models.GetDeltasInput{SinceMs: 0})
	assertSyncServiceBadRequest(t, err)

	_, err = sync_service.SyncActivity(ctx, 1, sync_models.SyncActivityInput{ClientUpdatedAt: -1})
	assertSyncServiceBadRequest(t, err)

	_, err = sync_service.SyncActivity(ctx, 1, sync_models.SyncActivityInput{ReadUpToID: -1})
	assertSyncServiceBadRequest(t, err)

	_, err = sync_service.SyncIdentity(ctx, 1, sync_models.SyncIdentityInput{ProfileUpdatedAt: -1})
	assertSyncServiceBadRequest(t, err)

	_, err = sync_service.SyncInbox(ctx, 1, sync_models.SyncInboxInput{ClientUpdatedAt: -1})
	assertSyncServiceBadRequest(t, err)

	_, err = sync_service.SyncMessages(ctx, 1, sync_models.SyncMessagesInput{ConversationID: 0, SinceMs: 0})
	assertSyncServiceBadRequest(t, err)

	_, err = sync_service.SyncMessages(ctx, 1, sync_models.SyncMessagesInput{ConversationID: 10, SinceMs: -1})
	assertSyncServiceBadRequest(t, err)

	finalKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)
	assert.ElementsMatch(t, initialKeys, finalKeys)
}
