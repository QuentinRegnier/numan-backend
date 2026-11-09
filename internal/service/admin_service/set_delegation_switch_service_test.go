package admin_service

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetDelegationSwitchService(t *testing.T) {
	ctx := context.Background()

	// Interrupteur de délégation basculé par un Admin
	switchRes, errSwitch := SetDelegationSwitchService(ctx, variables.UserGradeAdmin, true)
	require.NoError(t, errSwitch)
	assert.True(t, switchRes.Enabled)
	assert.True(t, variables.ModerationAdminDelegationEnabled)

	// Rejet si un modérateur normal tente de basculer l'interrupteur
	_, errForbiddenSwitch := SetDelegationSwitchService(ctx, variables.UserGradeModerator, false)
	require.Error(t, errForbiddenSwitch)
}
