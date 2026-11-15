package saved_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/saved_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestSavedModels_Validate(t *testing.T) {
	t.Run("SavedPayload", func(t *testing.T) {
		var nilP *saved_models.SavedPayload
		assertBadRequest(t, nilP.Validate())

		valid := saved_models.SavedPayload{
			ID:        1,
			UserID:    2,
			PostID:    3,
			CreatedAt: 1700000000000,
		}
		require.NoError(t, valid.Validate())

		bad := valid
		bad.ID = 0
		assertBadRequest(t, bad.Validate())

		bad = valid
		bad.UserID = -1
		assertBadRequest(t, bad.Validate())

		bad = valid
		bad.PostID = 0
		assertBadRequest(t, bad.Validate())

		bad = valid
		bad.CreatedAt = -1
		assertBadRequest(t, bad.Validate())
	})

	t.Run("SaveActionInput", func(t *testing.T) {
		var nilIn *saved_models.SaveActionInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&saved_models.SaveActionInput{PostID: 0}).Validate())
		assertBadRequest(t, (&saved_models.SaveActionInput{PostID: -5}).Validate())
		require.NoError(t, (&saved_models.SaveActionInput{PostID: 42}).Validate())
	})

	t.Run("GetSavedInput", func(t *testing.T) {
		var nilIn *saved_models.GetSavedInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&saved_models.GetSavedInput{Offset: -1}).Validate())
		assertBadRequest(t, (&saved_models.GetSavedInput{Limit: -1}).Validate())
		assertBadRequest(t, (&saved_models.GetSavedInput{Limit: 101}).Validate())
		badBefore := int64(0)
		assertBadRequest(t, (&saved_models.GetSavedInput{BeforeID: &badBefore}).Validate())
		goodBefore := int64(12)
		require.NoError(t, (&saved_models.GetSavedInput{Limit: 50, Offset: 0, BeforeID: &goodBefore}).Validate())
	})
}
