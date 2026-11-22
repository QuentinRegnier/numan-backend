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

func TestGetCommunityRecommendationsInput_ValidateAndDefaults(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *community_recommendation_models.GetCommunityRecommendationsInput
	assertBadRequest(t, nilIn.Validate())

	// Empty input -> defaults (limit=20, offset=0)
	empty := community_recommendation_models.GetCommunityRecommendationsInput{}
	require.NoError(t, empty.Validate())
	assert.Equal(t, 20, empty.EffectiveLimit())
	assert.Equal(t, 0, empty.EffectiveOffset())

	// Valid bounds
	valid := community_recommendation_models.GetCommunityRecommendationsInput{
		Limit:  50,
		Offset: 500,
		Lang:   "EN",
	}
	require.NoError(t, valid.Validate())
	assert.Equal(t, 50, valid.EffectiveLimit())
	assert.Equal(t, 500, valid.EffectiveOffset())

	// Invalid limits
	assertBadRequest(t, (&community_recommendation_models.GetCommunityRecommendationsInput{Limit: -1}).Validate())
	assertBadRequest(t, (&community_recommendation_models.GetCommunityRecommendationsInput{Limit: 51}).Validate())

	// Invalid offsets
	assertBadRequest(t, (&community_recommendation_models.GetCommunityRecommendationsInput{Offset: -1}).Validate())
	assertBadRequest(t, (&community_recommendation_models.GetCommunityRecommendationsInput{Offset: 501}).Validate())

	// Invalid languages
	assertBadRequest(t, (&community_recommendation_models.GetCommunityRecommendationsInput{Lang: "jp"}).Validate())
	assertBadRequest(t, (&community_recommendation_models.GetCommunityRecommendationsInput{Lang: "un"}).Validate())
	assertBadRequest(t, (&community_recommendation_models.GetCommunityRecommendationsInput{Lang: "fr-FR"}).Validate())
}

func TestGetCommunityRecommendationsOutput_NoIsJoinedField(t *testing.T) {
	out := community_recommendation_models.GetCommunityRecommendationsOutput{
		Lang:    "fr",
		Total:   1,
		HasMore: false,
		Communities: []community_recommendation_models.CommunityRecommendationItem{
			{
				ID:            101,
				Name:          "Tech FR",
				Description:   "Communauté tech",
				AvatarMediaID: 55,
				Lang:          "fr",
				Mode:          "free_chat",
				MemberCount:   42,
				IsPartner:     true,
				IsPinned:      true,
				PinSlot:       2,
				FriendsCount:  3,
			},
		},
	}

	raw, err := json.Marshal(out)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "is_joined")
	assert.Contains(t, string(raw), `"pin_slot":2`)
}
