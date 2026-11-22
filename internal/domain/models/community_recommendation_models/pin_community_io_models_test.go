package community_recommendation_models_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPinCommunityInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilPin *community_recommendation_models.PinCommunityInput
	assertBadRequest(t, nilPin.Validate())
	assertBadRequest(t, (&community_recommendation_models.PinCommunityInput{ConversationID: 0, Slot: 1}).Validate())
	assertBadRequest(t, (&community_recommendation_models.PinCommunityInput{ConversationID: -5, Slot: 1}).Validate())
	assertBadRequest(t, (&community_recommendation_models.PinCommunityInput{ConversationID: 10, Slot: 0}).Validate())
	assertBadRequest(t, (&community_recommendation_models.PinCommunityInput{ConversationID: 10, Slot: 101}).Validate())

	require.NoError(t, (&community_recommendation_models.PinCommunityInput{ConversationID: 10, Slot: 1}).Validate())
	require.NoError(t, (&community_recommendation_models.PinCommunityInput{ConversationID: 10, Slot: 100, ForceReplace: true}).Validate())

	out := community_recommendation_models.PinCommunityOutput{ConversationID: 10, Lang: "fr", PinSlot: 1}
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "displaced_conversation_id")
}
