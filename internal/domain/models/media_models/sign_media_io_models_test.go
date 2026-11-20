package media_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/media_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignMediaInput_Validate(t *testing.T) {
	assertBadRequestCode := func(t *testing.T, err error, code string) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, code, appErr.Code)
	}

	require.NoError(t, (&media_models.SignMediaInput{MediaID: 1}).Validate())
	require.NoError(t, (&media_models.SignMediaInput{MediaID: 1, PostID: 10}).Validate())
	require.NoError(t, (&media_models.SignMediaInput{MediaID: 1, ConversationID: 20}).Validate())

	var nilPtr *media_models.SignMediaInput
	assertBadRequestCode(t, nilPtr.Validate(), numan_error.CodeInvalidPayload)
	assertBadRequestCode(t, (&media_models.SignMediaInput{MediaID: 0}).Validate(), numan_error.CodeInvalidPayload)
	assertBadRequestCode(t, (&media_models.SignMediaInput{MediaID: 1, PostID: -5}).Validate(), numan_error.CodeInvalidPayload)
	assertBadRequestCode(t, (&media_models.SignMediaInput{MediaID: 1, ConversationID: -5}).Validate(), numan_error.CodeInvalidPayload)
	assertBadRequestCode(t, (&media_models.SignMediaInput{MediaID: 1, PostID: 10, ConversationID: 20}).Validate(), "AMBIGUOUS_CONTEXT")
}
