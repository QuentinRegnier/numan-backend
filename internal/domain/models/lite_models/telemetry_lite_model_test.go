package lite_models_test

import (
	"math"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelemetryProfileLite_Validate(t *testing.T) {
	assertBadRequest := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var appErr *numan_error.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
	}

	valid := lite_models.TelemetryProfileLite{
		Vector:          make([]float32, variables.VectorDimTotal),
		TopTags:         []string{"go"},
		ConfidenceScore: 0.75,
		TimestampMs:     100,
	}
	require.NoError(t, valid.Validate())

	var nilTel *lite_models.TelemetryProfileLite
	assertBadRequest(t, nilTel.Validate())
	assertBadRequest(t, (&lite_models.TelemetryProfileLite{Vector: []float32{0.1}}).Validate())
	assertBadRequest(t, (&lite_models.TelemetryProfileLite{ConfidenceScore: math.NaN()}).Validate())
	assertBadRequest(t, (&lite_models.TelemetryProfileLite{TimestampMs: -1}).Validate())
}
