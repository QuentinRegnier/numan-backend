package sync_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/sync_models"
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

func TestSyncModels_Validate(t *testing.T) {
	t.Run("FocusConversationInput", func(t *testing.T) {
		var nilIn *sync_models.FocusConversationInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&sync_models.FocusConversationInput{ConversationID: 0}).Validate())
		require.NoError(t, (&sync_models.FocusConversationInput{ConversationID: 10, Active: true}).Validate())
	})

	t.Run("GetDeltasInput", func(t *testing.T) {
		var nilIn *sync_models.GetDeltasInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&sync_models.GetDeltasInput{SinceMs: -1}).Validate())
		require.NoError(t, (&sync_models.GetDeltasInput{SinceMs: 0}).Validate())
		require.NoError(t, (&sync_models.GetDeltasInput{SinceMs: 1700000000000}).Validate())
	})

	t.Run("SyncActivityInput", func(t *testing.T) {
		var nilIn *sync_models.SyncActivityInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&sync_models.SyncActivityInput{ClientUpdatedAt: -1}).Validate())
		assertBadRequest(t, (&sync_models.SyncActivityInput{ReadUpToID: -1}).Validate())
		require.NoError(t, (&sync_models.SyncActivityInput{ClientUpdatedAt: 0, ReadUpToID: 0}).Validate())
	})

	t.Run("SyncIdentityInput", func(t *testing.T) {
		var nilIn *sync_models.SyncIdentityInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&sync_models.SyncIdentityInput{ProfileUpdatedAt: -1}).Validate())
		assertBadRequest(t, (&sync_models.SyncIdentityInput{SettingsUpdatedAt: -1}).Validate())
		require.NoError(t, (&sync_models.SyncIdentityInput{ProfileUpdatedAt: 10, SettingsUpdatedAt: 20}).Validate())
	})

	t.Run("SyncInboxInput", func(t *testing.T) {
		var nilIn *sync_models.SyncInboxInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&sync_models.SyncInboxInput{ClientUpdatedAt: -1}).Validate())
		require.NoError(t, (&sync_models.SyncInboxInput{ClientUpdatedAt: 0}).Validate())
	})

	t.Run("SyncMessagesInput", func(t *testing.T) {
		var nilIn *sync_models.SyncMessagesInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&sync_models.SyncMessagesInput{ConversationID: 0, SinceMs: 0}).Validate())
		assertBadRequest(t, (&sync_models.SyncMessagesInput{ConversationID: 1, SinceMs: -1}).Validate())
		require.NoError(t, (&sync_models.SyncMessagesInput{ConversationID: 1, SinceMs: 0}).Validate())
	})

	t.Run("SyncPresenceInput", func(t *testing.T) {
		var nilIn *sync_models.SyncPresenceInput
		assertBadRequest(t, nilIn.Validate())
		assertBadRequest(t, (&sync_models.SyncPresenceInput{UserIDs: nil}).Validate())
		assertBadRequest(t, (&sync_models.SyncPresenceInput{UserIDs: []int64{1, 0}}).Validate())
		tooMany := make([]int64, 51)
		for i := range tooMany {
			tooMany[i] = int64(i + 1)
		}
		assertBadRequest(t, (&sync_models.SyncPresenceInput{UserIDs: tooMany}).Validate())
		require.NoError(t, (&sync_models.SyncPresenceInput{UserIDs: []int64{1, 2, 3}}).Validate())
	})
}
