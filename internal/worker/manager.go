package worker

import (
	"context"
	"sync"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// ############################################################################
// # MANAGER : CHEF D'ORCHESTRE DE L'ASYNCHRONISME
// ############################################################################

// StartBackgroundWorkers lance tous les processus asynchrones vitaux de l'API numan.
// Il déploie les planificateurs (Crons) et les pools de travailleurs (Sharding) qui
// dépilent les files d'attente Redis H24.
func StartBackgroundWorkers(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du moteur de persistance et des processus asynchrones...")

	// ── ÉTAPE 1 : LANCEMENT DES MOTEURS ALGORITHMIQUES (CRONS) ──────────────

	// Moteur de Time-Decay : Recalcule la chute du score viral au fil du temps
	startScoreUpdaterCron(ctx)

	// Moteur d'Émergence : Fusionne les hashtags similaires (Fautes de frappe)
	startHashtagCanonCron(ctx)

	// Moteur de Tendances : Calcule le classement mondial des hashtags
	startHashtagTrendCron(ctx)

	// Moteur de Warm-up : Régénère en silence les flux des utilisateurs inactifs
	startFeedWarmupCron(ctx)

	// ── ÉTAPE 2 : LANCEMENT DES GARBAGE COLLECTORS (PURGES L3->L2) ──────────

	// Détruit les fichiers physiques et logs des médias orphelins
	startMediaCleanupCron(ctx)

	// Nettoie les Likes qui pointent vers des posts supprimés
	startLikeCleanupCron(ctx)

	// Nettoie les Réactions (Emoji) sur les messages supprimés
	startReactionCleanupCron(ctx)

	// Supprime les sauvegardes de posts qui n'existent plus
	startSavedCleanupCron(ctx)

	// ── ÉTAPE 3 : LANCEMENT DES CANAUX EXTERNES ─────────────────────────────

	// Initialise Firebase Cloud Messaging et consomme les requêtes de Push
	startPushNotificationWorker(ctx)

	// Initialise le client SMTP et consomme les requêtes d'Emails Sortants
	go startEmailWorker(ctx)

	// ── ÉTAPE 4 : DÉPLOIEMENT DU WORKER POOL (SHARDING) ─────────────────────
	// L'infrastructure asynchrone repose sur 64 Shards Redis pour annuler
	// la contention (Locking) et traiter massivement les flux d'E/S en parallèle.

	var wg sync.WaitGroup

	for i := 0; i < redis.QueueShards; i++ {
		wg.Add(1)

		// Chaque Goroutine gère exclusivement son propre Shard
		go func(shardID int) {
			defer wg.Done()
			runWorker(ctx, shardID)
		}(i)
	}

	// Note : L'attente du WaitGroup (wg.Wait) n'est pas bloquante ici,
	// car le maintien en vie de l'application est géré directement par le serveur
	// HTTP principal (Gin) dans cmd/main.go. Les workers tourneront tant que
	// le contexte global ne recevra pas le signal d'arrêt (ctx.Done).
	numan_log.Info(ctx).Msg("Moteur de persistance asynchrone opérationnel. Les 64 Workers sont à l'écoute.")
}
