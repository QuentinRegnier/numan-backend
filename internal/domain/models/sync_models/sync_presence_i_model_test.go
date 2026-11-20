package sync_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/sync_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncPresenceInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var nilIn *sync_models.SyncPresenceInput
	assertBadRequest(t, nilIn.Validate())
	assertBadRequest(t, (&sync_models.SyncPresenceInput{UserIDs: nil}).Validate())
	assertBadRequest(t, (&sync_models.SyncPresenceInput{UserIDs: []int64{}}).Validate())
	assertBadRequest(t, (&sync_models.SyncPresenceInput{UserIDs: []int64{1, 0}}).Validate())
	assertBadRequest(t, (&sync_models.SyncPresenceInput{UserIDs: []int64{1, -5}}).Validate())

	tooMany := make([]int64, 51)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	assertBadRequest(t, (&sync_models.SyncPresenceInput{UserIDs: tooMany}).Validate())

	maxValid := make([]int64, 50)
	for i := range maxValid {
		maxValid[i] = int64(i + 1)
	}
	require.NoError(t, (&sync_models.SyncPresenceInput{UserIDs: maxValid}).Validate())
	require.NoError(t, (&sync_models.SyncPresenceInput{UserIDs: []int64{1, 2, 3}}).Validate())
}
