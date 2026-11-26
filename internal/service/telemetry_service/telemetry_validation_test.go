package telemetry_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/telemetry_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/telemetry_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessSyncTelemetry_ValidationAndSideEffectGuard(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	var validInput telemetry_models.SyncTelemetryInput
	validInput.Payload.SchemaVersion = 1
	validInput.Payload.Vector.Dims = variables.VectorDimTotal
	validInput.Payload.Vector.Values = make([]float32, variables.VectorDimTotal)
	validInput.Payload.Meta.ConfidenceScore = 0.2
	validInput.Payload.Meta.ClientUpdatedAt = 1700000000000
	validInput.Payload.TopTags = []string{"go"}
	validInput.Payload.Telemetry = []telemetry_models.TelemetryEvent{
		{PostID: 1001, DwellTimeMs: 2500, IsClicked: true},
	}

	// Unauthenticated callerID <= 0
	_, err := telemetry_service.ProcessSyncTelemetry(ctx, 0, validInput)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)

	// Invalid vector dimensions
	badVec := validInput
	badVec.Payload.Vector.Values = []float32{0.1, 0.2}
	_, err = telemetry_service.ProcessSyncTelemetry(ctx, 10, badVec)
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)

	// Invalid telemetry event (negative dwell time)
	badEvent := validInput
	badEvent.Payload.Telemetry = []telemetry_models.TelemetryEvent{{PostID: 1001, DwellTimeMs: -50}}
	_, err = telemetry_service.ProcessSyncTelemetry(ctx, 10, badEvent)
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)

	keys, errKeys := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, errKeys)
	assert.Empty(t, keys)

	// Valid request succeeds and writes telemetry profile & queue entry
	out, errValid := telemetry_service.ProcessSyncTelemetry(ctx, 10, validInput)
	require.NoError(t, errValid)
	assert.Equal(t, "accepted", out.Status)

	keysAfter, errKeysAfter := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, errKeysAfter)
	assert.NotEmpty(t, keysAfter)
}
