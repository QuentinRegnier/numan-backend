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

func TestCreateCommunityInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *conversation_models.CreateCommunityInput
	assertBadRequest(t, nilIn.Validate())
	require.NoError(t, (&conversation_models.CreateCommunityInput{Title: "Numan Dev"}).Validate())
	assertBadRequest(t, (&conversation_models.CreateCommunityInput{Title: "ab"}).Validate())
	assertBadRequest(t, (&conversation_models.CreateCommunityInput{Title: strings.Repeat("é", 101)}).Validate())
	assertBadRequest(t, (&conversation_models.CreateCommunityInput{Title: "Numan", OwnerID: -1}).Validate())
	assertBadRequest(t, (&conversation_models.CreateCommunityInput{
		Title:    "Numan",
		Settings: conversation_models.ConversationSettings{WritePermission: -1},
	}).Validate())
	assertBadRequest(t, (&conversation_models.CreateCommunityInput{Title: "Numan", ExternalLink: models.ExternalLinks{URL: "bad"}}).Validate())
}
