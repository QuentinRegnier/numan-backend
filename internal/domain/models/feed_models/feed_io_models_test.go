package feed_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/feed_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetFeedInput_Validate(t *testing.T) {
	t.Run("nil receiver fails", func(t *testing.T) {
		var p *feed_models.GetFeedInput
		err := p.Validate()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	})

	t.Run("negative LastSeenIndex fails", func(t *testing.T) {
		err := (&feed_models.GetFeedInput{LastSeenIndex: -1}).Validate()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	})

	t.Run("zero and positive LastSeenIndex pass", func(t *testing.T) {
		require.NoError(t, (&feed_models.GetFeedInput{LastSeenIndex: 0}).Validate())
		require.NoError(t, (&feed_models.GetFeedInput{Force: true, LastSeenIndex: 25}).Validate())
	})
}
