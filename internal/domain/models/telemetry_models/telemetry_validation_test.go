package telemetry_models_test

import (
	"math"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/telemetry_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
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

func validSyncPayload() telemetry_models.SyncTelemetryPayload {
	var p telemetry_models.SyncTelemetryPayload
	p.SchemaVersion = 1
	p.Vector.Dims = variables.VectorDimTotal
	p.Vector.EmbeddingVersion = 1
	p.Vector.Values = make([]float32, variables.VectorDimTotal)
	p.Vector.Values[0] = 0.5
	p.Vector.NormPreUnit = 1.0
	p.Meta.TotalInteractions = 10
	p.Meta.ConfidenceScore = 0.5
	p.Meta.ClientPlatform = "ios"
	p.Meta.ClientUpdatedAt = 1700000000000
	p.TopTags = []string{"golang", "tech"}
	p.Telemetry = []telemetry_models.TelemetryEvent{
		{PostID: 123, DwellTimeMs: 2000, IsClicked: true},
	}
	return p
}

func TestTelemetryEvent_Validate(t *testing.T) {
	require.NoError(t, (&telemetry_models.TelemetryEvent{PostID: 1, DwellTimeMs: 0}).Validate())

	var nilPtr *telemetry_models.TelemetryEvent
	assertBadRequest(t, nilPtr.Validate())
	assertBadRequest(t, (&telemetry_models.TelemetryEvent{PostID: 0, DwellTimeMs: 100}).Validate())
	assertBadRequest(t, (&telemetry_models.TelemetryEvent{PostID: 1, DwellTimeMs: -1}).Validate())
}

func TestSyncTelemetryPayloadAndInput_Validate(t *testing.T) {
	valid := validSyncPayload()
	require.NoError(t, valid.Validate())
	require.NoError(t, (&telemetry_models.SyncTelemetryInput{Payload: valid}).Validate())

	var nilPayload *telemetry_models.SyncTelemetryPayload
	assertBadRequest(t, nilPayload.Validate())
	var nilInput *telemetry_models.SyncTelemetryInput
	assertBadRequest(t, nilInput.Validate())

	cases := []struct {
		name   string
		mutate func(*telemetry_models.SyncTelemetryPayload)
	}{
		{"negative schema version", func(p *telemetry_models.SyncTelemetryPayload) { p.SchemaVersion = -1 }},
		{"negative dims", func(p *telemetry_models.SyncTelemetryPayload) { p.Vector.Dims = -1 }},
		{"wrong vector length", func(p *telemetry_models.SyncTelemetryPayload) { p.Vector.Values = []float32{0.1, 0.2} }},
		{"mismatched dims field", func(p *telemetry_models.SyncTelemetryPayload) { p.Vector.Dims = 128 }},
		{"nan in vector", func(p *telemetry_models.SyncTelemetryPayload) { p.Vector.Values[3] = float32(math.NaN()) }},
		{"negative norm", func(p *telemetry_models.SyncTelemetryPayload) { p.Vector.NormPreUnit = -0.1 }},
		{"negative total interactions", func(p *telemetry_models.SyncTelemetryPayload) { p.Meta.TotalInteractions = -1 }},
		{"confidence above 1", func(p *telemetry_models.SyncTelemetryPayload) { p.Meta.ConfidenceScore = 1.2 }},
		{"nan confidence", func(p *telemetry_models.SyncTelemetryPayload) { p.Meta.ConfidenceScore = math.NaN() }},
		{"negative client timestamp", func(p *telemetry_models.SyncTelemetryPayload) { p.Meta.ClientUpdatedAt = -10 }},
		{"blank top tag", func(p *telemetry_models.SyncTelemetryPayload) { p.TopTags = []string{"   "} }},
		{"too many events", func(p *telemetry_models.SyncTelemetryPayload) {
			p.Telemetry = make([]telemetry_models.TelemetryEvent, 1001)
		}},
		{"invalid event in batch", func(p *telemetry_models.SyncTelemetryPayload) {
			p.Telemetry = []telemetry_models.TelemetryEvent{{PostID: 0, DwellTimeMs: 500}}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validSyncPayload()
			tc.mutate(&p)
			assertBadRequest(t, p.Validate())
		})
	}
}
