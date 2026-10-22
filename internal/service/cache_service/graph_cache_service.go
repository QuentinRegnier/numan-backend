package cache_service

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/vmihailenco/msgpack/v5"
)

// ############################################################################
// # DTOs ET MODÈLES DU CACHE
// ############################################################################

// tagEdge représente la valeur binaire (MessagePack) stockée dans le HASH Redis.
type tagEdge struct {
	Weight    float64 `msgpack:"w"` // Poids W de l'arête sémantique
	Timestamp int64   `msgpack:"t"` // Timestamp de la dernière occurrence en millisecondes
}

// ############################################################################
// # SERVICE : GRAPHE SÉMANTIQUE ET CO-OCCURRENCES (MARKOV)
// ############################################################################

// UpdateTagCooccurrences calcule et met à jour les poids markoviens pour toutes les paires de tags.
func UpdateTagCooccurrences(ctx context.Context, tagsList []string, currentTimestampMs int64) {
	if len(tagsList) < 2 {
		return // Pas de co-occurrence possible avec moins de 2 tags.
	}

	// Nettoyage et uniformisation des tags pour éviter la duplication.
	var cleanedTagsList []string
	for _, tag := range tagsList {
		cleanedTag := strings.ToLower(strings.TrimSpace(tag))
		if cleanedTag != "" {
			cleanedTagsList = append(cleanedTagsList, cleanedTag)
		}
	}

	// Génération de toutes les paires uniques (Graphe non orienté)
	for i := 0; i < len(cleanedTagsList); i++ {
		for j := i + 1; j < len(cleanedTagsList); j++ {
			sourceTag := cleanedTagsList[i]
			targetTag := cleanedTagsList[j]

			// Mise à jour de l'arête A -> B
			updateEdge(ctx, sourceTag, targetTag, currentTimestampMs)
			// Mise à jour de l'arête B -> A (Symétrie pour lecture O(1) depuis n'importe quel sommet)
			updateEdge(ctx, targetTag, sourceTag, currentTimestampMs)
		}
	}
}

// updateEdge exécute l'équation d'état markovienne :
// W_nouveau = (W_ancien * e^(-λ * Δt)) * (1 - α) + α
func updateEdge(ctx context.Context, sourceTag, targetTag string, currentTimestampMs int64) {
	var currentEdge tagEdge

	// 1. Lecture de l'état actuel via la Collection abstraite Redis
	binaryData, errRedis := redis.GraphEdges.HGet(ctx, sourceTag, targetTag).Bytes()
	if errRedis == nil {
		err := msgpack.Unmarshal(binaryData, &currentEdge)
		if err != nil {
			numan_log.Error(ctx).Err(err).Msg("Échec de la désérialisation MessagePack pour une arête de graphe")
			return
		}
	}

	// 2. Calcul du Δt en jours
	var deltaDays float64 = 0
	if currentEdge.Timestamp > 0 {
		deltaDays = float64(currentTimestampMs-currentEdge.Timestamp) / (1000 * 60 * 60 * 24)
		if deltaDays < 0 {
			deltaDays = 0 // Sécurité anti-voyage dans le temps
		}
	}

	// 3. Moteur Mathématique d'Apprentissage et d'Oubli
	decayedWeight := currentEdge.Weight * math.Exp(-variables.GraphDecayLambda*deltaDays)
	newComputedWeight := decayedWeight*(1.0-variables.GraphLearningAlpha) + variables.GraphLearningAlpha

	// 4. Sérialisation et Sauvegarde
	currentEdge.Weight = newComputedWeight
	currentEdge.Timestamp = currentTimestampMs

	newBinaryData, errMarshal := msgpack.Marshal(&currentEdge)
	if errMarshal != nil {
		numan_log.Error(ctx).Err(errMarshal).Msg("Échec de la sérialisation MessagePack pour une arête de graphe")
		return
	}

	errSet := redis.GraphEdges.HSet(ctx, sourceTag, targetTag, newBinaryData)
	if errSet != nil {
		numan_log.Error(ctx).Err(errSet).Msg("Échec de l'écriture Redis pour le graphe de Markov")
	}
}

// GetRelatedTagsLazy retourne les tags cousins et déclenche l'élagage paresseux (Lazy Pruning).
func GetRelatedTagsLazy(ctx context.Context, sourceTag string) map[string]float64 {
	sanitizedSourceTag := strings.ToLower(strings.TrimSpace(sourceTag))

	edgesMapJSON, errRedis := redis.GraphEdges.HGetAll(ctx, sanitizedSourceTag).Result()
	if errRedis != nil || len(edgesMapJSON) == 0 {
		return nil
	}

	survivingEdgesMap := make(map[string]float64)
	var edgesToDeleteList []string
	currentTimestampMs := time.Now().UnixMilli()

	// Évaluation à la volée de la vitalité des arêtes sémantiques
	for targetTag, binaryData := range edgesMapJSON {
		var currentEdge tagEdge
		if errUnmarshal := msgpack.Unmarshal([]byte(binaryData), &currentEdge); errUnmarshal != nil {
			continue
		}

		deltaDays := float64(currentTimestampMs-currentEdge.Timestamp) / (1000 * 60 * 60 * 24)
		currentWeight := currentEdge.Weight * math.Exp(-variables.GraphDecayLambda*deltaDays)

		if currentWeight < variables.GraphSurvivalEps {
			// L'arête est morte (Poids sous le seuil ε)
			edgesToDeleteList = append(edgesToDeleteList, targetTag)
		} else {
			// L'arête survit et est retournée au système
			survivingEdgesMap[targetTag] = currentWeight
		}
	}

	// Élagage Asynchrone (HDel) : Auto-nettoyage de la RAM sans ralentir la requête utilisateur
	if len(edgesToDeleteList) > 0 {
		go func(targetsToPrune []string) {
			backgroundCtx := context.Background()
			errPrune := redis.GraphEdges.HDel(backgroundCtx, sanitizedSourceTag, targetsToPrune...)
			if errPrune != nil {
				numan_log.Warn(ctx).Err(errPrune).Msg("Échec de l'élagage paresseux (Lazy Pruning) dans le Graph Cache")
			}
		}(edgesToDeleteList)
	}

	return survivingEdgesMap
}

// SeedGraphCache réalise le Cold Start (Time-Travel Ingestion) de l'écosystème sémantique
// lors d'un déploiement ou d'une remise à zéro du cache.
func SeedGraphCache(ctx context.Context) error {
	numan_log.Info(ctx).Msg("Début de l'initialisation du graphe sémantique (Graph Cache)...")

	// 1. Récupération de l'historique complet (Filtré : > 1 tag, trié par date croissante)
	historicalPosts, errPg := postgres.FuncLoadPostsForGraphSeeding(ctx)
	if errPg != nil {
		return errPg
	}

	numan_log.Info(ctx).Int("count", len(historicalPosts)).Msg("Publications trouvées pour le rejeu temporel sémantique.")

	// 2. Rejeu Temporel pour reconstruire fidèlement l'état d'apprentissage
	for _, postRecord := range historicalPosts {
		UpdateTagCooccurrences(ctx, postRecord.Hashtags, postRecord.CreatedAt.UnixMilli())
	}

	numan_log.Info(ctx).Msg("Graphe sémantique initialisé avec succès !")
	return nil
}
