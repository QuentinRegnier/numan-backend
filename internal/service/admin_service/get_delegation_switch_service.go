package admin_service

import (
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// GetDelegationSwitchService lit l'état actuel de l'interrupteur de délégation.
func GetDelegationSwitchService() bool {
	return variables.ModerationAdminDelegationEnabled
}
