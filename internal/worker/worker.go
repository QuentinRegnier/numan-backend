package worker

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
)

// ============================================================================
// CONSTANTES DE CONFIGURATION DU CERVEAU ASYNCHRONE
// ============================================================================
var (
	// Limite dynamique du nombre d'événements traités en un seul cycle
	maxBatchSize int64 = 5000

	// Backoff Minimum (Période d'hyperactivité : vitesse de scrutation max)
	minBackoff = 50 * time.Millisecond

	// Backoff Maximum (Sommeil profond : pour économiser le CPU si la file est vide)
	maxBackoff = 1 * time.Second
)

// init surcharge les variables par défaut si elles sont définies dans le .env
func init() {
	if val, err := pkg.ParseInt64Strict(os.Getenv("WORKER_MAX_BATCH_SIZE")); err == nil && val > 0 {
		maxBatchSize = val
	}
	if val, err := strconv.Atoi(os.Getenv("WORKER_MIN_BACKOFF_MS")); err == nil && val > 0 {
		minBackoff = time.Duration(val) * time.Millisecond
	}
	if val, err := strconv.Atoi(os.Getenv("WORKER_MAX_BACKOFF_MS")); err == nil && val > 0 {
		maxBackoff = time.Duration(val) * time.Millisecond
	}
}

// ############################################################################
// # WORKER PRINCIPAL : MOTEUR DE DÉPILEMENT ET DE DISPATCH
// ############################################################################

// runWorker est la boucle infinie exécutée par chaque Shard.
// Elle consomme les événements Redis et applique un backoff exponentiel en cas d'inactivité.
func runWorker(ctx context.Context, shardID int) {
	currentBackoff := minBackoff

	for {
		// ── ÉTAPE 1 : ÉCOUTE DU SIGNAL D'ARRÊT GRACIEUX ─────────────────────
		select {
		case <-ctx.Done():
			numan_log.Info(ctx).Int("shard_id", shardID).Msg("Worker : Arrêt gracieux de la boucle de consommation.")
			return
		default:
		}

		// ── ÉTAPE 2 : DÉPILEMENT BLOQUANT (BLMPOP) ──────────────────────────
		// Le worker se met en pause (0 CPU) jusqu'à ce qu'un événement arrive
		// ou que le timeout de Redis soit atteint.
		events, err := redis.PopSmartBatchBlocking(ctx, shardID, maxBatchSize)
		if err != nil {
			numan_log.Error(ctx).Err(err).Int("shard_id", shardID).Msg("Worker Redis : Échec critique lors du dépilement (BLMPOP)")
			time.Sleep(1 * time.Second) // Temporisation de sécurité en cas de crash réseau
			continue
		}

		// ── ÉTAPE 3 : TRAITEMENT ET GESTION DE L'ÉNERGIE (BACKOFF) ──────────
		if len(events) > 0 {
			processBatch(ctx, events)

			// RESET DU SOMMEIL : on a trouvé du travail, on repasse en hyperactivité !
			currentBackoff = minBackoff
		} else {
			// SLEEP : la file était vide (malgré le blocage initial), on s'endort doucement.
			time.Sleep(currentBackoff)
			currentBackoff *= 2
			if currentBackoff > maxBackoff {
				currentBackoff = maxBackoff
			}
		}
	}
}

// processBatch orchestre le traitement d'un lot d'événements. Il filtre, route vers les BDD,
// attend leur validation, puis met à jour les algorithmes en RAM.
func processBatch(ctx context.Context, events []redis.AsyncEvent) {

	// ── ÉTAPE 1 : BOUCLIER DE SÉCURITÉ (PURIFICATION ASYNCHRONE) ────────────
	// On purge les événements illégaux ou corrompus AVANT de les distribuer.
	validEvents := purifyBatch(ctx, events)
	if len(validEvents) == 0 {
		return // Tout a été rejeté par le pare-feu
	}

	// ── ÉTAPE 2 : ROUTAGE VERS LES BASES DE DONNÉES ─────────────────────────
	var mongoEvents []redis.AsyncEvent
	var pgEvents []redis.AsyncEvent

	for _, evt := range validEvents {
		if evt.Targets&redis.TargetMongo != 0 {
			mongoEvents = append(mongoEvents, evt)
		}
		if evt.Targets&redis.TargetPostgres != 0 {
			pgEvents = append(pgEvents, evt)
		}
	}

	// ── ÉTAPE 3 : PERSISTANCE CONCOURANTE (L2 & L3) ─────────────────────────
	var wg sync.WaitGroup

	if len(mongoEvents) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			flushMongo(ctx, mongoEvents)
		}()
	}

	if len(pgEvents) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			flushPostgres(ctx, pgEvents)
		}()
	}

	// ── ÉTAPE 4 : BARRIÈRE DE SYNCHRONISATION ───────────────────────────────
	// On DOIT attendre que la BDD (L2/L3) ait validé les transactions sur le disque.
	// Si on mettait à jour le cache algorithmique avant, une erreur BDD créerait
	// une désynchronisation permanente entre la RAM et le disque.
	wg.Wait()

	// ── ÉTAPE 5 : HYDRATATION DES CACHES ALGORITHMIQUES (L1) ────────────────
	// À cet instant, on est certain que le disque est à jour.

	// 1. Le Cerveau (ZSETs et Vecteurs de Recommandations ML)
	go updateMostCache(ctx, validEvents)

	// 2. Fan-Out Social de masse (Distribution dans les boîtes aux lettres L1)
	handleSocialFanOut(ctx, validEvents)

	// 3. Mise à jour du Graphe Sémantique (Émergence Collective de Markov)
	handleGraphUpdate(ctx, validEvents)
}

// ============================================================================
// PARE-FEU ASYNCHRONE (BOUCLIER DE SÉCURITÉ)
// ============================================================================

// purifyBatch élimine les événements illégaux. Il garantit qu'un utilisateur banni
// ou n'ayant pas les droits ne puisse pas forcer des incrémentations de compteurs en BDD.
func purifyBatch(ctx context.Context, events []redis.AsyncEvent) []redis.AsyncEvent {
	validEvents := make([]redis.AsyncEvent, 0, len(events))

	for _, e := range events {

		// On inspecte exclusivement les événements d'interactions (vulnérables au brigading)
		if e.Type == redis.EntityLike || e.Type == redis.EntityView || e.Type == redis.EntityComment {
			jsonBytes, errMarshal := json.Marshal(e.Payload)
			if errMarshal != nil {
				continue // Payload corrompu, événement détruit
			}

			// Extraction générique des IDs cibles
			var payload struct {
				PostID   int64 `json:"post_id"`
				TargetID int64 `json:"target_id"` // Historique
				UserID   int64 `json:"user_id"`
			}

			if errUnmarshal := json.Unmarshal(jsonBytes, &payload); errUnmarshal == nil {
				targetID := payload.TargetID
				if payload.PostID != 0 {
					targetID = payload.PostID
				}

				if targetID != 0 && payload.UserID != 0 {
					// ── CONTRÔLE DE SÉCURITÉ (CASCADE L1 -> L2 -> L3) ───────────────
					// On doit s'assurer que le Post existe toujours et récupérer ses règles.
					// (getPostWithFallback est défini dans most_cache_worker.go)
					p, errFallback := getPostWithFallback(ctx, targetID)

					// Règle 1 : Post supprimé (Soft Delete ou inexistant)
					if errFallback != nil || p.Visibility == -1 {
						continue // L'interaction cible un fantôme -> Événement détruit
					}

					// Règle 2 : Application de la Matrice de Confidentialité
					isAuthor := p.UserID == payload.UserID
					if !isAuthor {
						relationState := cache_service.RelationValue(ctx, p.UserID, payload.UserID)

						// A. L'utilisateur est bloqué par l'auteur
						if relationState == -1 {
							continue
						}
						// B. Le post est réservé aux Abonnés et l'utilisateur ne l'est pas
						if p.Visibility == 1 && relationState < 1 {
							continue
						}
						// C. Le post est réservé aux Amis et l'utilisateur ne l'est pas
						if p.Visibility == 2 && relationState != 2 {
							continue
						}
						// D. Visibilité non supportée (ancien mode privé ou hors limites)
						if p.Visibility < 0 || p.Visibility > 2 {
							continue
						}
					}
				}
			} else {
				continue // Structure du payload invalide
			}
		}

		// L'événement a survécu au pare-feu (ou ne nécessitait pas d'inspection), on l'accepte
		validEvents = append(validEvents, e)
	}

	return validEvents
}
