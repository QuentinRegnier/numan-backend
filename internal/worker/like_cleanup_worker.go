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
// # WORKER : LIKE CLEANUP (GARBAGE COLLECTOR DES LIKES ORPHELINS)
// ############################################################################

// startLikeCleanupCron lance le Garbage Collector qui détruit les likes orphelins.
// Un like devient orphelin lorsque la publication (Post) ou le Commentaire qu'il ciblait a été supprimé.
func startLikeCleanupCron(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du Garbage Collector de Likes (Cron 6h)...")

	go func() {
		ticker := time.NewTicker(variables.LikeCleanupCronInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return // Arrêt gracieux
			case <-ticker.C:
				runWithDistributedCronLock(ctx, "cron:like_cleanup", 10*time.Minute, processLikeCleanup)
			}
		}
	}()
}

// processLikeCleanup centralise la logique de purge en cascade (L3 -> L2).
func processLikeCleanup(ctx context.Context) {
	// ── ÉTAPE 1 : IDENTIFICATION ET PURGE L3 (POSTGRESQL - SOURCE DE VÉRITÉ) ──
	// PostgreSQL exécute la jointure pour trouver les likes sans cible existante
	// et les supprime. Il nous retourne la liste de ce qu'il a supprimé.
	orphanTargets, errPg := postgres.FuncDeleteOrphanLikes(ctx)
	if errPg != nil {
		numan_log.Error(ctx).Err(errPg).Msg("Garbage Collector Likes : Échec critique de la purge PostgreSQL (L3)")
		return
	}

	if len(orphanTargets) == 0 {
		return // Rien à nettoyer ce cycle, on s'arrête ici.
	}

	numan_log.Info(ctx).Int("count", len(orphanTargets)).Msg("Garbage Collector Likes : Purge L3 terminée. Répercussion sur le L2 en cours...")

	// ── ÉTAPE 2 : TRI PAR TYPE DE CIBLE POUR LA PURGE NOSQL ───────────────────
	var postIDs []int64
	var commentIDs []int64

	for _, target := range orphanTargets {
		// TargetType = 0 (Post), TargetType = 1 (Commentaire)
		if target.TargetType == 0 {
			postIDs = append(postIDs, target.TargetID)
		} else if target.TargetType == 1 {
			commentIDs = append(commentIDs, target.TargetID)
		}
	}

	// ── ÉTAPE 3 : PURGE DU WARM STORAGE L2 (MONGODB) ──────────────────────────
	// On doit répercuter cette suppression physique sur Mongo pour éviter
	// que le Cache L2 ne renvoie des likes fantômes lors d'un Fallback.
	if mongo.Likes != nil {
		// Purge des likes de posts
		if len(postIDs) > 0 {
			_, errMongoPost := mongo.Likes.DB.Collection(mongo.Likes.Name).DeleteMany(ctx, bson.M{
				"target_type": 0,
				"target_id":   bson.M{"$in": postIDs},
			})
			if errMongoPost != nil {
				numan_log.Error(ctx).Err(errMongoPost).Msg("Garbage Collector Likes : Échec de la suppression Mongo (Posts)")
			}
		}

		// Purge des likes de commentaires
		if len(commentIDs) > 0 {
			_, errMongoComment := mongo.Likes.DB.Collection(mongo.Likes.Name).DeleteMany(ctx, bson.M{
				"target_type": 1,
				"target_id":   bson.M{"$in": commentIDs},
			})
			if errMongoComment != nil {
				numan_log.Error(ctx).Err(errMongoComment).Msg("Garbage Collector Likes : Échec de la suppression Mongo (Commentaires)")
			}
		}
	}
}
