package variables

import "time"

// ============================================================================
// PARAMÈTRES DYNAMIQUES DES WORKERS GARBAGE COLLECTOR
// ============================================================================

var (
	LikeCleanupCronInterval     = 6 * time.Hour // Fréquence de nettoyage des likes orphelins
	MediaCleanupCronInterval    = 1 * time.Hour // Fréquence de nettoyage des fichiers médias orphelins
	ReactionCleanupCronInterval = 6 * time.Hour // Fréquence de nettoyage des réactions orphelines
	SavedCleanupCronInterval    = 6 * time.Hour // Fréquence de nettoyage des favoris orphelins
)
