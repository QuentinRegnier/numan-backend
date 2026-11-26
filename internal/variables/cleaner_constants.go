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

	// Phase 4 : Nettoyage asynchrone des clés orphelines Redis (SCAN + L3 Postgres + UNLINK)
	RedisOrphanCleanerDryRun     = true             // Mode simulation sécurisé par défaut (aucun UNLINK tant que true)
	RedisOrphanGraceWindow       = 15 * time.Minute // Fenêtre de grâce anti-race Write-Behind basée sur le timestamp Snowflake
	RedisOrphanScanCount         = 200              // Taille de page SCAN Redis non-bloquante
	RedisOrphanPostgresBatchSize = 500              // Nombre d'IDs vérifiés par requête SQL batch sur ReadDB
	RedisOrphanUnlinkBatchSize   = 200              // Nombre de clés supprimées par lot UNLINK Cluster-safe
	RedisOrphanMaxKeysPerCycle   = 50000            // Plafond maximal de clés scannées par exécution
)
