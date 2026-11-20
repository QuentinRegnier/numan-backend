package telemetry_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/telemetry_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetryEvent_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	require.NoError(t, (&telemetry_models.TelemetryEvent{PostID: 1, DwellTimeMs: 0}).Validate())

	var nilPtr *telemetry_models.TelemetryEvent
	assertBadRequest(t, nilPtr.Validate())
	assertBadRequest(t, (&telemetry_models.TelemetryEvent{PostID: 0, DwellTimeMs: 100}).Validate())
	assertBadRequest(t, (&telemetry_models.TelemetryEvent{PostID: 1, DwellTimeMs: -1}).Validate())
}
