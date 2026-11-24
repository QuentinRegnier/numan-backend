package worker

import (
	"context"
)

// Exports dédiés à l'Activity-Aware Job Scheduler (internal/service/scheduler_service)
// permettant d'exécuter les anciens crons périodiques sous contrôle du régime d'activité DC
// (préemption coopérative + verrouillage LocalDC / GlobalSQL) au lieu de tickers aveugles.

// RunHashtagCanonicalizationCycle exécute une passe de canonicalisation Levenshtein des hashtags.
func RunHashtagCanonicalizationCycle(ctx context.Context) {
	processHashtagCanonicalization(ctx)
}

// RunFeedWarmupCycle exécute une passe de pré-chauffage de flux pour les utilisateurs programmés.
func RunFeedWarmupCycle(ctx context.Context) {
	processScheduledWarmups(ctx)
}

// RunMediaCleanupCycle exécute une passe de purge des médias orphelins (L3 -> S3/MinIO).
func RunMediaCleanupCycle(ctx context.Context) {
	processMediaCleanup(ctx)
}

// RunLikeCleanupCycle exécute une passe de purge des likes orphelins (L3 -> L2).
func RunLikeCleanupCycle(ctx context.Context) {
	processLikeCleanup(ctx)
}

// RunReactionCleanupCycle exécute une passe de purge des réactions orphelines (L3 -> L2).
func RunReactionCleanupCycle(ctx context.Context) {
	processReactionCleanup(ctx)
}

// RunSavedCleanupCycle exécute une passe de purge des favoris/sauvegardes orphelins (L3 -> L2).
func RunSavedCleanupCycle(ctx context.Context) {
	processSavedCleanup(ctx)
}
