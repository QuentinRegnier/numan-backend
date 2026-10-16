package variables

import "time"

// ============================================================================
// CONSTANTES ET PARAMÈTRES DU WORKER D'INTERACTIONS (MESSAGERIE)
// ============================================================================

const (
	// Capacité du channel allouée une seule fois au démarrage du package (init)
	InteractionBufferSize = 50000
)

var (
	InteractionBatchThreshold = 5000            // Seuil de déclenchement du vidage anticipé (Viralité)
	InteractionFlushInterval  = 5 * time.Second // Intervalle de vidage automatique du tampon
)
