package admin_service

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
)

func TestGetDelegationSwitchService(t *testing.T) {
	_ = context.Background()

	// Par défaut, l'interrupteur est false
	variables.ModerationAdminDelegationEnabled = false
	assert.False(t, GetDelegationSwitchService())

	// Bascule sur true
	variables.ModerationAdminDelegationEnabled = true
	assert.True(t, GetDelegationSwitchService())
}
