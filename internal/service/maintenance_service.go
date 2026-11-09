package service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"go.mongodb.org/mongo-driver/bson"
)

// ############################################################################
// # MAINTENANCE ET NETTOYAGE DES CACHES (GARBAGE COLLECTION)
// ############################################################################

// cleanMongo effectue une purge temporelle glissante (Sliding TTL) sur le Warm Storage.
// Tous les documents non accédés ("last_use") depuis 30 jours sont évincés.
func cleanMongo() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	recentDatabase := mongo.MongoClient.Database("numan_recent")

	// 1. Découverte dynamique de toutes les collections
	collectionNames, err := recentDatabase.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Échec de la découverte des collections Mongo pour la purge")
		return
	}

	// 2. Le seuil de péremption : 30 jours dans le passé
	expirationThreshold := domain.NowTime().AddDate(0, 0, -30)
	deletionFilter := bson.M{
		"last_use": bson.M{
			"$lt": expirationThreshold,
		},
	}

	// 3. Purge itérative
	for _, collectionName := range collectionNames {
		targetCollection := recentDatabase.Collection(collectionName)

		deletionResult, errDelete := targetCollection.DeleteMany(ctx, deletionFilter)
		if errDelete != nil {
			numan_log.Error(ctx).Err(errDelete).Str("collection", collectionName).Msg("Échec de la purge du cache glissant Mongo")
			continue
		}

		if deletionResult.DeletedCount > 0 {
			numan_log.Info(ctx).
				Str("collection", collectionName).
				Int64("deleted_count", deletionResult.DeletedCount).
				Msg("Purge glissante L2 Mongo réussie")
		}
	}
}

// cleanRedis vide l'intégralité du cache L1. Utilisé uniquement en environnement de Dev ou lors d'un Hard Reset.
func cleanRedis() {
	// Sécurité anti-crash unifiée via la couche d'accès
	if !redis.IsReady() {
		numan_log.Warn(context.Background()).Msg("Nettoyage ignoré : Connexion Redis non initialisée (Rdb est nil).")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := redis.FlushDB(ctx)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("Échec critique du Flush Redis")
		return
	}

	numan_log.Info(ctx).Msg("Cache volatil Redis (L1) vidé avec succès.")
}

// InitData orchestre le grand nettoyage au démarrage du serveur si le flag CLEAN_DB_ON_STARTUP est actif.
func InitData() {
	numan_log.Info(context.Background()).Msg("Début de la séquence de Hard Reset : Nettoyage L1 (Redis) et L2 (Mongo)...")
	cleanMongo()
	cleanRedis()
	numan_log.Info(context.Background()).Msg("Séquence de Hard Reset terminée avec succès.")
}
