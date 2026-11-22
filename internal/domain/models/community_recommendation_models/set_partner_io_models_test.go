package community_recommendation_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/community_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetPartnerInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilPartner *community_recommendation_models.SetPartnerInput
	assertBadRequest(t, nilPartner.Validate())
	assertBadRequest(t, (&community_recommendation_models.SetPartnerInput{ConversationID: 0, IsPartner: true}).Validate())
	assertBadRequest(t, (&community_recommendation_models.SetPartnerInput{ConversationID: -3, IsPartner: false}).Validate())
	require.NoError(t, (&community_recommendation_models.SetPartnerInput{ConversationID: 10, IsPartner: false}).Validate())
	require.NoError(t, (&community_recommendation_models.SetPartnerInput{ConversationID: 10, IsPartner: true}).Validate())
}
