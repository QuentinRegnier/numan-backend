package moderation_models_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModBatchLikeInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	assertBadRequest(t, (*moderation_models.ModBatchLikeInput)(nil).Validate())
	assertBadRequest(t, (&moderation_models.ModBatchLikeInput{AuthorUserIDs: nil}).Validate())
	assertBadRequest(t, (&moderation_models.ModBatchLikeInput{AuthorUserIDs: []int64{1, -5}}).Validate())
	assertBadRequest(t, (&moderation_models.ModBatchLikeInput{AuthorUserIDs: []int64{1, 0}}).Validate())
	assertBadRequest(t, (&moderation_models.ModBatchLikeInput{AuthorUserIDs: make([]int64, 501)}).Validate())
	assertBadRequest(t, (&moderation_models.ModBatchLikeInput{AuthorUserIDs: []int64{1}, Reason: strings.Repeat("r", 501)}).Validate())
	assert.NoError(t, (&moderation_models.ModBatchLikeInput{AuthorUserIDs: []int64{1, 2}, Reason: "Qualité"}).Validate())
}
