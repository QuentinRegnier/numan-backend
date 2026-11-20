package notification_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotificationPayload_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilP *notification_models.NotificationPayload
	assertBadRequest(t, nilP.Validate())

	valid := notification_models.NotificationPayload{
		ID:       1,
		UserID:   10,
		ActorID:  20,
		Type:     "post_like",
		TargetID: 300,
	}
	require.NoError(t, valid.Validate())

	assertBadRequest(t, (&notification_models.NotificationPayload{UserID: 0, ActorID: 20, Type: "post_like"}).Validate())
	assertBadRequest(t, (&notification_models.NotificationPayload{UserID: 10, ActorID: 0, Type: "post_like"}).Validate())
	assertBadRequest(t, (&notification_models.NotificationPayload{UserID: 10, ActorID: 10, Type: "post_like"}).Validate())
	assertBadRequest(t, (&notification_models.NotificationPayload{UserID: 10, ActorID: 20, Type: "   "}).Validate())
	assertBadRequest(t, (&notification_models.NotificationPayload{UserID: 10, ActorID: 20, Type: "post_like", TargetID: -1}).Validate())
}
