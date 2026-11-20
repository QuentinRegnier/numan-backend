package notification_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/notification_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadNotificationsInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *notification_models.ReadNotificationsInput
	assertBadRequest(t, nilIn.Validate())
	assertBadRequest(t, (&notification_models.ReadNotificationsInput{ReadUpToID: 0}).Validate())
	assertBadRequest(t, (&notification_models.ReadNotificationsInput{ReadUpToID: -5}).Validate())
	require.NoError(t, (&notification_models.ReadNotificationsInput{ReadUpToID: 999}).Validate())
}
