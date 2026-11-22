package conversation_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateConversationInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *conversation_models.UpdateConversationInput
	assertBadRequest(t, nilIn.Validate())
	require.NoError(t, (&conversation_models.UpdateConversationInput{ConversationID: 1, Title: "Nouveau titre"}).Validate())
	assertBadRequest(t, (&conversation_models.UpdateConversationInput{ConversationID: 0}).Validate())
	assertBadRequest(t, (&conversation_models.UpdateConversationInput{ConversationID: 1, Title: strings.Repeat("é", 101)}).Validate())
	assertBadRequest(t, (&conversation_models.UpdateConversationInput{ConversationID: 1, Description: strings.Repeat("é", 501)}).Validate())
	assertBadRequest(t, (&conversation_models.UpdateConversationInput{ConversationID: 1, AvatarID: -5}).Validate())
	assertBadRequest(t, (&conversation_models.UpdateConversationInput{
		ConversationID: 1,
		Settings:       conversation_models.ConversationSettings{WritePermission: 3},
	}).Validate())
	assertBadRequest(t, (&conversation_models.UpdateConversationInput{
		ConversationID: 1,
		ExternalLink:   models.ExternalLinks{URL: "ftp://bad"},
	}).Validate())
	require.NoError(t, (&conversation_models.UpdateConversationInput{ConversationID: 1, Lang: "fr"}).Validate())
	require.NoError(t, (&conversation_models.UpdateConversationInput{ConversationID: 1, Lang: " EN "}).Validate())
	assertBadRequest(t, (&conversation_models.UpdateConversationInput{ConversationID: 1, Lang: "xx"}).Validate())
	assertBadRequest(t, (&conversation_models.UpdateConversationInput{ConversationID: 1, Lang: "fr-FR"}).Validate())
}
