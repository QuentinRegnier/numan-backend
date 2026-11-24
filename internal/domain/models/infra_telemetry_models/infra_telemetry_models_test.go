package infra_telemetry_models

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInfraTelemetryModels_NormalizeAndValidate(t *testing.T) {
	t.Run("NormalizeCountryCode rejects non ISO-2 and IPs", func(t *testing.T) {
		assert.Equal(t, "FR", NormalizeCountryCode("fr"))
		assert.Equal(t, "BR", NormalizeCountryCode(" BR "))
		assert.Equal(t, "ZZ", NormalizeCountryCode("XX"))
		assert.Equal(t, "ZZ", NormalizeCountryCode("T1"))
		assert.Equal(t, "ZZ", NormalizeCountryCode("192.168.1.1"))
		assert.Equal(t, "ZZ", NormalizeCountryCode("FRA"))
		assert.Equal(t, "ZZ", NormalizeCountryCode(""))
	})

	t.Run("PodHeartbeat validation", func(t *testing.T) {
		var nilHB *PodHeartbeat
		require.Error(t, nilHB.Validate())
		require.Error(t, (&PodHeartbeat{Datacenter: "dc1"}).Validate())
		require.NoError(t, (&PodHeartbeat{Datacenter: "dc1", PodName: "api-0"}).Validate())
	})

	t.Run("CLITelemetrySnapshot validation", func(t *testing.T) {
		var nilSnap *CLITelemetrySnapshot
		require.Error(t, nilSnap.Validate())
		require.Error(t, (&CLITelemetrySnapshot{Datacenter: "dc1", SchedulerRegime: scheduler_models.Regime("INVALID")}).Validate())
		require.NoError(t, (&CLITelemetrySnapshot{Datacenter: "dc1", SchedulerRegime: scheduler_models.RegimeIdle}).Validate())
	})
}
