package worker

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"go.mongodb.org/mongo-driver/bson"
)

// ############################################################################
// # WORKER : SAVED CLEANUP (GARBAGE COLLECTOR DES SAUVEGARDES ORPHELINES)
// ############################################################################

// startSavedCleanupCron lance le Garbage Collector qui détruit les favoris orphelins.
// Un post sauvegardé devient orphelin s'il est soft-deleted ou hard-deleted par son auteur.
func startSavedCleanupCron(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du Garbage Collector de Favoris (Cron 6h)...")

	go func() {
		ticker := time.NewTicker(variables.SavedCleanupCronInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return // Arrêt gracieux
			case <-ticker.C:
				runWithDistributedCronLock(ctx, "cron:saved_cleanup", 10*time.Minute, processSavedCleanup)
			}
		}
	}()
}

// processSavedCleanup orchestre la purge des favoris obsolètes en base de données.
func processSavedCleanup(ctx context.Context) {
	// ── ÉTAPE 1 : IDENTIFICATION ET PURGE L3 (POSTGRESQL - SOURCE DE VÉRITÉ) ──
	// PostgreSQL exécute la jointure pour trouver les enregistrements de "save"
	// dont le post parent n'existe plus ou est masqué (visibility < 0 ou = 2).
	orphanPostIDs, errPg := postgres.FuncDeleteOrphanSaved(ctx)
	if errPg != nil {
		numan_log.Error(ctx).Err(errPg).Msg("Garbage Collector Favoris : Échec critique de la purge PostgreSQL (L3)")
		return
	}

	if len(orphanPostIDs) == 0 {
		return // Rien à nettoyer ce cycle
	}

	numan_log.Info(ctx).Int("count", len(orphanPostIDs)).Msg("Garbage Collector Favoris : Purge L3 terminée. Répercussion sur le L2 en cours...")

	// ── ÉTAPE 2 : PURGE DU WARM STORAGE L2 (MONGODB) ──────────────────────────
	// Afin de maintenir la cohérence des données et éviter les "PostNotFound" côté client,
	// on supprime physiquement les références orphelines du cache secondaire.
	if mongo.Saved != nil {
		_, errMongo := mongo.Saved.DB.Collection(mongo.Saved.Name).DeleteMany(ctx, bson.M{
			"post_id": bson.M{"$in": orphanPostIDs},
		})

		if errMongo != nil {
			numan_log.Error(ctx).Err(errMongo).Msg("Garbage Collector Favoris : Échec de la suppression sur MongoDB (L2)")
		}
	}
}
