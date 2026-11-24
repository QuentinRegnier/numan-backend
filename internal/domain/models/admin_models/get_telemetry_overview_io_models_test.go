package admin_models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTelemetryOverviewInput_Validate(t *testing.T) {
	t.Run("nil and empty are valid", func(t *testing.T) {
		var nilIn *GetTelemetryOverviewInput
		require.NoError(t, nilIn.Validate())
		require.NoError(t, (&GetTelemetryOverviewInput{}).Validate())
	})

	t.Run("valid datacenter normalized", func(t *testing.T) {
		in := &GetTelemetryOverviewInput{Datacenter: "  eu-west-1  "}
		require.NoError(t, in.Validate())
		assert.Equal(t, "eu-west-1", in.Datacenter)
	})

	t.Run("invalid datacenter characters or length rejected", func(t *testing.T) {
		require.Error(t, (&GetTelemetryOverviewInput{Datacenter: "dc1; DROP TABLE"}).Validate())
		require.Error(t, (&GetTelemetryOverviewInput{Datacenter: strings.Repeat("a", 65)}).Validate())
	})
}
