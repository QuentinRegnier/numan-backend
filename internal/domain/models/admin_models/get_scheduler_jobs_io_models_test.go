package admin_models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSchedulerJobsInput_Validate(t *testing.T) {
	var nilIn *GetSchedulerJobsInput
	require.NoError(t, nilIn.Validate())
	require.NoError(t, (&GetSchedulerJobsInput{}).Validate())

	valid := &GetSchedulerJobsInput{Datacenter: " dc-fr-01 "}
	require.NoError(t, valid.Validate())
	assert.Equal(t, "dc-fr-01", valid.Datacenter)

	require.Error(t, (&GetSchedulerJobsInput{Datacenter: "dc/../../etc"}).Validate())
}
