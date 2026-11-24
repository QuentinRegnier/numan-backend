package admin_models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTriggerSchedulerJobInput_Validate(t *testing.T) {
	var nilIn *TriggerSchedulerJobInput
	require.Error(t, nilIn.Validate())

	require.Error(t, (&TriggerSchedulerJobInput{}).Validate())
	require.Error(t, (&TriggerSchedulerJobInput{JobName: "INVALID-JOB!"}).Validate())

	valid := &TriggerSchedulerJobInput{JobName: "  hashtag_canon  ", Force: true}
	require.NoError(t, valid.Validate())
	assert.Equal(t, "hashtag_canon", valid.JobName)
}
