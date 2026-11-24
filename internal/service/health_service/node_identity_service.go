package health_service

import (
	"sync"
	"sync/atomic"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/health_models"
)

var (
	shuttingDown atomic.Bool
	identityMu   sync.RWMutex
	nodeIdentity health_models.NodeIdentity
)

// SetNodeIdentity enregistre l'identité opérationnelle du nœud exposée sur /healthz et /readyz.
func SetNodeIdentity(datacenter, role string, nodeID int64) {
	identityMu.Lock()
	nodeIdentity = health_models.NodeIdentity{
		Datacenter: datacenter,
		Role:       role,
		NodeID:     nodeID,
	}
	identityMu.Unlock()
}

// CurrentNodeIdentity retourne l'identité opérationnelle courante du nœud.
func CurrentNodeIdentity() health_models.NodeIdentity {
	identityMu.RLock()
	id := nodeIdentity
	identityMu.RUnlock()
	return id
}

// SetShuttingDown bascule l'état de drainage du pod (fait répondre 503 sur /readyz).
func SetShuttingDown(v bool) {
	shuttingDown.Store(v)
}

// IsShuttingDown indique si le pod est en cours d'arrêt gracieux.
func IsShuttingDown() bool {
	return shuttingDown.Load()
}
