package conversation_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetWatermarksInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *conversation_models.GetWatermarksInput
	assertBadRequest(t, nilIn.Validate())
	assertBadRequest(t, (&conversation_models.GetWatermarksInput{ConversationID: 0}).Validate())
	assertBadRequest(t, (&conversation_models.GetWatermarksInput{ConversationID: -1}).Validate())
	require.NoError(t, (&conversation_models.GetWatermarksInput{ConversationID: 12}).Validate())
}
