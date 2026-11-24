package cache_service

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	goredis "github.com/redis/go-redis/v9"
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
// Paramètre optionnel lang ...int : partitionne le graphe sémantique par langue.
func UpdateTagCooccurrences(ctx context.Context, tagsList []string, currentTimestampMs int64, lang ...int) {
	if len(tagsList) < 2 {
		return // Pas de co-occurrence possible avec moins de 2 tags.
	}

	resolvedLang := variables.DefaultContentLanguage
	hasLang := len(lang) > 0
	if hasLang {
		resolvedLang = variables.NormalizeLangForCache(lang[0])
	}

	// Nettoyage, dédoublonnage et uniformisation des tags.
	seen := make(map[string]struct{}, len(tagsList))
	var cleanedTagsList []string
	for _, tag := range tagsList {
		cleanedTag := strings.ToLower(strings.TrimSpace(tag))
		if cleanedTag == "" {
			continue
		}
		if _, exists := seen[cleanedTag]; !exists {
			seen[cleanedTag] = struct{}{}
			cleanedTagsList = append(cleanedTagsList, cleanedTag)
		}
	}

	if len(cleanedTagsList) < 2 {
		return
	}

	// Génération de toutes les paires uniques (Graphe non orienté)
	for i := 0; i < len(cleanedTagsList); i++ {
		for j := i + 1; j < len(cleanedTagsList); j++ {
			sourceTag := cleanedTagsList[i]
			targetTag := cleanedTagsList[j]

			// Mise à jour de l'arête A -> B et B -> A dans la partition linguistique
			updateEdge(ctx, variables.GraphEdgesSubKey(resolvedLang, sourceTag), targetTag, currentTimestampMs)
			updateEdge(ctx, variables.GraphEdgesSubKey(resolvedLang, targetTag), sourceTag, currentTimestampMs)

			// Maintien de la clé non-préfixée pour rétrocompatibilité des appels sans langue
			updateEdge(ctx, sourceTag, targetTag, currentTimestampMs)
			updateEdge(ctx, targetTag, sourceTag, currentTimestampMs)
		}
	}
}

// updateEdge exécute l'équation d'état markovienne :
// W_nouveau = (W_ancien * e^(-λ * Δt)) * (1 - α) + α
func updateEdge(ctx context.Context, sourceKey, targetTag string, currentTimestampMs int64) {
	var currentEdge tagEdge

	// 1. Lecture de l'état actuel via la Collection abstraite Redis
	binaryData, errRedis := redis.GraphEdges.HGet(ctx, sourceKey, targetTag).Bytes()
	if errRedis == nil {
		err := msgpack.Unmarshal(binaryData, &currentEdge)
		if err != nil {
			numan_log.Error(ctx).Err(err).Msg("Échec de la désérialisation MessagePack pour une arête de graphe")
			return
		}
	} else if !errors.Is(errRedis, goredis.Nil) {
		numan_log.Warn(ctx).Err(errRedis).Str("source_key", sourceKey).Str("target_tag", targetTag).Msg("Échec de la lecture Redis pour une arête du graphe de Markov")
		return
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

	errSet := redis.GraphEdges.HSet(ctx, sourceKey, targetTag, newBinaryData)
	if errSet != nil {
		numan_log.Error(ctx).Err(errSet).Msg("Échec de l'écriture Redis pour le graphe de Markov")
	}
}

// GetRelatedTagsLazy retourne les tags cousins et déclenche l'élagage paresseux (Lazy Pruning).
// Paramètre optionnel lang ...int : interroge la partition linguistique du graphe sémantique.
func GetRelatedTagsLazy(ctx context.Context, sourceTag string, lang ...int) map[string]float64 {
	sanitizedSourceTag := strings.ToLower(strings.TrimSpace(sourceTag))
	if sanitizedSourceTag == "" {
		return nil
	}

	lookupKey := sanitizedSourceTag
	if len(lang) > 0 {
		resolvedLang := variables.NormalizeLangForCache(lang[0])
		langKey := variables.GraphEdgesSubKey(resolvedLang, sanitizedSourceTag)
		edgesMapJSON, errRedis := redis.GraphEdges.HGetAll(ctx, langKey).Result()
		if errRedis == nil && len(edgesMapJSON) > 0 {
			return evaluateAndPruneEdges(ctx, langKey, edgesMapJSON)
		}
	}

	edgesMapJSON, errRedis := redis.GraphEdges.HGetAll(ctx, lookupKey).Result()
	if errRedis != nil {
		numan_log.Warn(ctx).Err(errRedis).Str("tag", lookupKey).Msg("Échec de la lecture HGetAll du Graph Cache")
		return nil
	}
	if len(edgesMapJSON) == 0 {
		return nil
	}

	return evaluateAndPruneEdges(ctx, lookupKey, edgesMapJSON)
}

func evaluateAndPruneEdges(ctx context.Context, lookupKey string, edgesMapJSON map[string]string) map[string]float64 {
	survivingEdgesMap := make(map[string]float64)
	var edgesToDeleteList []string
	currentTimestampMs := domain.NowMillis()

	// Évaluation à la volée de la vitalité des arêtes sémantiques
	for targetTag, binaryData := range edgesMapJSON {
		var currentEdge tagEdge
		if errUnmarshal := msgpack.Unmarshal([]byte(binaryData), &currentEdge); errUnmarshal != nil {
			numan_log.Warn(ctx).Err(errUnmarshal).Str("target_tag", targetTag).Msg("Échec de désérialisation MessagePack d'une arête dans GetRelatedTagsLazy")
			continue
		}

		deltaDays := float64(currentTimestampMs-currentEdge.Timestamp) / (1000 * 60 * 60 * 24)
		if deltaDays < 0 {
			deltaDays = 0
		}
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
		go func(key string, targetsToPrune []string) {
			backgroundCtx := context.Background()
			errPrune := redis.GraphEdges.HDel(backgroundCtx, key, targetsToPrune...)
			if errPrune != nil {
				numan_log.Warn(backgroundCtx).Err(errPrune).Msg("Échec de l'élagage paresseux (Lazy Pruning) dans le Graph Cache")
			}
		}(lookupKey, edgesToDeleteList)
	}

	return survivingEdgesMap
}

// SeedGraphCache réalise le Cold Start (Time-Travel Ingestion) de l'écosystème sémantique
// lors d'un déploiement ou d'une remise à zéro du cache.
func SeedGraphCache(ctx context.Context) error {
	numan_log.Info(ctx).Msg("Début de l'initialisation du graphe sémantique (Graph Cache)...")

	// 1. Récupération de l'historique complet (Filtré : visibility = 0, > 1 tag, trié par date croissante)
	historicalPosts, errPg := postgres.FuncLoadPostsForGraphSeeding(ctx)
	if errPg != nil {
		return errPg
	}

	numan_log.Info(ctx).Int("count", len(historicalPosts)).Msg("Publications trouvées pour le rejeu temporel sémantique.")

	// 2. Rejeu Temporel par langue pour reconstruire fidèlement l'état d'apprentissage
	for _, postRecord := range historicalPosts {
		UpdateTagCooccurrences(ctx, postRecord.Hashtags, domain.TimeToMillis(postRecord.CreatedAt), postRecord.Lang)
	}

	numan_log.Info(ctx).Msg("Graphe sémantique initialisé avec succès !")
	return nil
}
