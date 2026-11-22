package community_recommendation_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnpinCommunityInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilUnpin *community_recommendation_models.UnpinCommunityInput
	assertBadRequest(t, nilUnpin.Validate())
	assertBadRequest(t, (&community_recommendation_models.UnpinCommunityInput{ConversationID: 0}).Validate())
	assertBadRequest(t, (&community_recommendation_models.UnpinCommunityInput{ConversationID: -1}).Validate())
	require.NoError(t, (&community_recommendation_models.UnpinCommunityInput{ConversationID: 42}).Validate())
}
