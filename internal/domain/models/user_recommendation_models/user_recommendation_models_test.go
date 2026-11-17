package user_recommendation_models_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.True(t, errors.As(err, &appErr), "attendu AppError, reçu: %v", err)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestGetUserRecommendationsInput_Validate(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var nilInput *user_recommendation_models.GetUserRecommendationsInput
		assertBadRequest(t, nilInput.Validate())
	})

	t.Run("defaults applied when empty", func(t *testing.T) {
		in := &user_recommendation_models.GetUserRecommendationsInput{}
		require.NoError(t, in.Validate())
		assert.Equal(t, variables.UserRecoSortGlobal, in.Sort)
		assert.Equal(t, variables.UserRecoDefaultLimit, in.Limit)
		assert.False(t, in.Force)
	})

	t.Run("all valid sort modes and limits", func(t *testing.T) {
		for _, sortMode := range []string{"global", "geo", "aff", "taste"} {
			in := &user_recommendation_models.GetUserRecommendationsInput{
				Sort:  sortMode,
				Limit: 50,
				Force: true,
			}
			require.NoError(t, in.Validate())
		}
		inMin := &user_recommendation_models.GetUserRecommendationsInput{
			Sort:  "global",
			Limit: 1,
		}
		require.NoError(t, inMin.Validate())
	})

	t.Run("invalid sort mode", func(t *testing.T) {
		in := &user_recommendation_models.GetUserRecommendationsInput{
			Sort:  "popular",
			Limit: 20,
		}
		assertBadRequest(t, in.Validate())
	})

	t.Run("invalid limits", func(t *testing.T) {
		assertBadRequest(t, (&user_recommendation_models.GetUserRecommendationsInput{Sort: "global", Limit: -1}).Validate())
		assertBadRequest(t, (&user_recommendation_models.GetUserRecommendationsInput{Sort: "global", Limit: 51}).Validate())
		assertBadRequest(t, (&user_recommendation_models.GetUserRecommendationsInput{Sort: "global", Limit: 1000}).Validate())
	})
}

func TestSkipUserRecommendationInput_Validate(t *testing.T) {
	var nilInput *user_recommendation_models.SkipUserRecommendationInput
	assertBadRequest(t, nilInput.Validate())

	assertBadRequest(t, (&user_recommendation_models.SkipUserRecommendationInput{SkippedUserID: 0}).Validate())
	assertBadRequest(t, (&user_recommendation_models.SkipUserRecommendationInput{SkippedUserID: -42}).Validate())

	valid := &user_recommendation_models.SkipUserRecommendationInput{SkippedUserID: 12345}
	require.NoError(t, valid.Validate())
}

func TestUserRecoSkipPayload_Validate(t *testing.T) {
	now := time.Now().UTC()

	var nilPayload *user_recommendation_models.UserRecoSkipPayload
	assertBadRequest(t, nilPayload.Validate())

	assertBadRequest(t, (&user_recommendation_models.UserRecoSkipPayload{UserID: 0, SkippedUserID: 2, CreatedAt: now}).Validate())
	assertBadRequest(t, (&user_recommendation_models.UserRecoSkipPayload{UserID: 1, SkippedUserID: 0, CreatedAt: now}).Validate())
	assertBadRequest(t, (&user_recommendation_models.UserRecoSkipPayload{UserID: 10, SkippedUserID: 10, CreatedAt: now}).Validate())
	assertBadRequest(t, (&user_recommendation_models.UserRecoSkipPayload{UserID: 1, SkippedUserID: 2}).Validate())

	valid := &user_recommendation_models.UserRecoSkipPayload{
		UserID:        1,
		SkippedUserID: 2,
		CreatedAt:     now,
	}
	require.NoError(t, valid.Validate())
}

func TestGenerateUserRecommendationsJobPayload_Validate(t *testing.T) {
	var nilJob *user_recommendation_models.GenerateUserRecommendationsJobPayload
	assertBadRequest(t, nilJob.Validate())
	assertBadRequest(t, (&user_recommendation_models.GenerateUserRecommendationsJobPayload{UserID: 0}).Validate())

	valid := &user_recommendation_models.GenerateUserRecommendationsJobPayload{
		UserID:      99,
		Force:       true,
		TriggeredBy: "http",
		RequestedAt: 1700000000000,
	}
	require.NoError(t, valid.Validate())
}
