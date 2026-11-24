package health_models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthModels_Validate(t *testing.T) {
	t.Run("NodeIdentity valid and invalid", func(t *testing.T) {
		var nilID *NodeIdentity
		require.Error(t, nilID.Validate())

		invalid := &NodeIdentity{NodeID: -1}
		require.Error(t, invalid.Validate())

		valid := &NodeIdentity{Datacenter: "dc1", Role: "api_node", NodeID: 2}
		require.NoError(t, valid.Validate())
	})

	t.Run("LivenessOutput valid and invalid", func(t *testing.T) {
		var nilOut *LivenessOutput
		require.Error(t, nilOut.Validate())

		empty := &LivenessOutput{}
		require.Error(t, empty.Validate())

		valid := &LivenessOutput{Status: "alive", Role: "monolith", Datacenter: "dc1", NodeID: 1}
		require.NoError(t, valid.Validate())
		assert.Equal(t, "alive", valid.Status)
	})

	t.Run("ReadinessOutput valid and invalid", func(t *testing.T) {
		var nilOut *ReadinessOutput
		require.Error(t, nilOut.Validate())

		empty := &ReadinessOutput{}
		require.Error(t, empty.Validate())

		valid := &ReadinessOutput{
			Status: "ready",
			Checks: map[string]string{"redis": "ok", "postgres": "ok", "mongo": "ok"},
		}
		require.NoError(t, valid.Validate())
	})
}
