package conversation_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuggestInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *conversation_models.SuggestInput
	assertBadRequest(t, nilIn.Validate())
	assertBadRequest(t, (&conversation_models.SuggestInput{ConversationID: -1}).Validate())
	assertBadRequest(t, (&conversation_models.SuggestInput{Query: strings.Repeat("é", 51)}).Validate())
	assertBadRequest(t, (&conversation_models.SuggestInput{Limit: 51}).Validate())
	assertBadRequest(t, (&conversation_models.SuggestInput{Limit: -1}).Validate())
	assertBadRequest(t, (&conversation_models.SuggestInput{Offset: -1}).Validate())
	assertBadRequest(t, (&conversation_models.SuggestInput{Intent: "invalid"}).Validate())

	for _, intent := range []string{"", "dm", "group", "tag", "mention"} {
		require.NoError(t, (&conversation_models.SuggestInput{
			ConversationID: 1,
			Query:          "alice",
			Limit:          20,
			Offset:         0,
			Intent:         intent,
		}).Validate())
	}
}
