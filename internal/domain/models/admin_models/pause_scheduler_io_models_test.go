package admin_models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPauseSchedulerInput_Validate(t *testing.T) {
	var nilIn *PauseSchedulerInput
	require.NoError(t, nilIn.Validate())
	require.NoError(t, (&PauseSchedulerInput{}).Validate())

	valid := &PauseSchedulerInput{TTLMinutes: 45, Reason: "  maintenance node drain  "}
	require.NoError(t, valid.Validate())
	assert.Equal(t, "maintenance node drain", valid.Reason)

	require.Error(t, (&PauseSchedulerInput{TTLMinutes: -5}).Validate())
	require.Error(t, (&PauseSchedulerInput{TTLMinutes: 1441}).Validate())
	require.Error(t, (&PauseSchedulerInput{Reason: strings.Repeat("x", 257)}).Validate())
}
