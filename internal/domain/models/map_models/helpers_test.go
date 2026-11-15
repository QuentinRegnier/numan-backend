package map_models_test

import (
	"math"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/stretchr/testify/require"
)

func TestValidateCoordinates(t *testing.T) {
	require.NoError(t, map_models.ValidateCoordinates(48.8566, 2.3522))
	require.NoError(t, map_models.ValidateCoordinates(-90.0, -180.0))
	require.NoError(t, map_models.ValidateCoordinates(90.0, 180.0))

	require.Error(t, map_models.ValidateCoordinates(90.001, 2.35))
	require.Error(t, map_models.ValidateCoordinates(-90.001, 2.35))
	require.Error(t, map_models.ValidateCoordinates(48.85, 180.001))
	require.Error(t, map_models.ValidateCoordinates(48.85, -180.001))
	require.Error(t, map_models.ValidateCoordinates(math.NaN(), 2.35))
	require.Error(t, map_models.ValidateCoordinates(48.85, math.Inf(1)))
}

func TestValidateBBox(t *testing.T) {
	require.NoError(t, map_models.ValidateBBox(48.80, 2.25, 48.90, 2.42))
	require.Error(t, map_models.ValidateBBox(48.95, 2.25, 48.90, 2.42))
	require.Error(t, map_models.ValidateBBox(48.80, 2.50, 48.90, 2.42))
	require.Error(t, map_models.ValidateBBox(-95.0, 2.25, 48.90, 2.42))
	require.Error(t, map_models.ValidateBBox(48.80, 2.25, 48.90, 190.0))
}
