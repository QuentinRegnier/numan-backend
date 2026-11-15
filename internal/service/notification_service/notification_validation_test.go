package notification_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/notification_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertNotifServiceBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestNotificationServices_InvalidPayloads_NoRedisSideEffects(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	initialKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)

	_, err = notification_service.GetNotifications(ctx, 1, notification_models.GetNotificationsInput{Limit: 101})
	assertNotifServiceBadRequest(t, err)

	_, err = notification_service.GetNotifications(ctx, 0, notification_models.GetNotificationsInput{Limit: 10})
	assertNotifServiceBadRequest(t, err)

	_, err = notification_service.MarkNotificationsAsRead(ctx, 1, notification_models.ReadNotificationsInput{ReadUpToID: 0})
	assertNotifServiceBadRequest(t, err)

	err = notification_service.DispatchNotification(ctx, 1, 2, "   ", 10)
	assertNotifServiceBadRequest(t, err)

	err = notification_service.DispatchNotification(ctx, 1, 0, "post_like", 10)
	assertNotifServiceBadRequest(t, err)

	err = notification_service.DispatchNotification(ctx, 1, 2, "post_like", -1)
	assertNotifServiceBadRequest(t, err)

	err = notification_service.DispatchNotification(ctx, 0, 2, "post_like", 10)
	assertNotifServiceBadRequest(t, err)

	finalKeys, err := infraRedis.Rdb.Keys(ctx, "*").Result()
	require.NoError(t, err)
	assert.ElementsMatch(t, initialKeys, finalKeys)

	queueKeys, err := infraRedis.Rdb.Keys(ctx, "request_cache:q:*").Result()
	require.NoError(t, err)
	assert.Empty(t, queueKeys)
}
