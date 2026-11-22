package worker

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # WORKER : MEDIA CLEANUP (GARBAGE COLLECTOR DES MÉDIAS S3/BDD)
// ############################################################################

// startMediaCleanupCron lance le Garbage Collector qui détruit les médias orphelins.
// Agit sur les brouillons expirés ou les médias rattachés à un contenu définitivement supprimé.
func startMediaCleanupCron(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du Garbage Collector de Médias (Cron 1h)...")

	go func() {
		ticker := time.NewTicker(variables.MediaCleanupCronInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return // Arrêt gracieux
			case <-ticker.C:
				runWithDistributedCronLock(ctx, "cron:media_cleanup", 10*time.Minute, processMediaCleanup)
			}
		}
	}()
}

// processMediaCleanup orchestre la destruction stricte et ordonnée d'un média (L3 -> S3 -> L1 -> L2).
func processMediaCleanup(ctx context.Context) {
	// ── ÉTAPE 1 : IDENTIFICATION ET PURGE L3 (POSTGRESQL - SOURCE DE VÉRITÉ) ──
	// La BDD relationnelle identifie les orphelins (selon les règles métier :
	// non référencés et délai de grâce expiré) et supprime l'entrée.
	orphans, errPg := postgres.FuncDeleteOrphanMedia(ctx)
	if errPg != nil {
		numan_log.Error(ctx).Err(errPg).Msg("Garbage Collector Médias : Échec critique de l'identification Postgres (L3)")
		return
	}

	if len(orphans) == 0 {
		return // Rien à nettoyer ce cycle.
	}

	numan_log.Info(ctx).Int("count", len(orphans)).Msg("Garbage Collector Médias : Médias orphelins purgés de Postgres (L3).")

	var idsToDelete []int64

	// ── ÉTAPE 2 : DESTRUCTION PHYSIQUE (S3/MINIO) ET ÉVICTION RAM (L1) ────────
	for _, orphan := range orphans {
		idsToDelete = append(idsToDelete, orphan.ID)

		// A. Destruction physique du fichier stocké (Cloud Storage)
		// On le fait avant Mongo pour éviter les fichiers "zombies" dans le bucket S3
		if orphan.StoragePath != "" {
			errS3 := media_service.RemovePhysicalMedia(ctx, orphan.StoragePath)
			if errS3 != nil {
				// On loggue mais on continue le processus pour ne pas bloquer les autres médias
				numan_log.Error(ctx).
					Err(errS3).
					Str("storage_path", orphan.StoragePath).
					Msg("Garbage Collector Médias : Échec de la destruction physique S3")
			}
		}

		// B. Destruction de l'empreinte en RAM (Object Cache L1)
		_ = object_cache_service.DeleteMediaFromObjectCache(ctx, orphan.ID)
	}

	// ── ÉTAPE 3 : PURGE DU WARM STORAGE L2 (MONGODB) ──────────────────────────
	if errMongo := mongo.MongoDeleteMediaByIDs(idsToDelete); errMongo != nil {
		numan_log.Error(ctx).Err(errMongo).Msg("Garbage Collector Médias : Échec de la purge MongoDB (L2)")
	}

	numan_log.Info(ctx).Int("count", len(orphans)).Msg("Garbage Collector Médias : Nettoyage complet en cascade (L3, S3, L1, L2) terminé avec succès.")
}
