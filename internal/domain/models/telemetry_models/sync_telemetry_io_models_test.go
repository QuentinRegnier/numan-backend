package telemetry_models_test

import (
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/telemetry_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncTelemetryInput_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	var valid telemetry_models.SyncTelemetryPayload
	valid.SchemaVersion = 1
	valid.Vector.Dims = variables.VectorDimTotal
	valid.Vector.EmbeddingVersion = 1
	valid.Vector.Values = make([]float32, variables.VectorDimTotal)
	valid.Vector.Values[0] = 0.5
	valid.Vector.NormPreUnit = 1.0
	valid.Meta.TotalInteractions = 10
	valid.Meta.ConfidenceScore = 0.5
	valid.Meta.ClientPlatform = "ios"
	valid.Meta.ClientUpdatedAt = 1700000000000
	valid.TopTags = []string{"golang", "tech"}
	valid.Telemetry = []telemetry_models.TelemetryEvent{
		{PostID: 123, DwellTimeMs: 2000, IsClicked: true},
	}

	require.NoError(t, (&telemetry_models.SyncTelemetryInput{Payload: valid}).Validate())

	var nilInput *telemetry_models.SyncTelemetryInput
	assertBadRequest(t, nilInput.Validate())

	invalid := valid
	invalid.SchemaVersion = -1
	assertBadRequest(t, (&telemetry_models.SyncTelemetryInput{Payload: invalid}).Validate())
}
