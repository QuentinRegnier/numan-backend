package algorithm_service

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/vmihailenco/msgpack/v5"
)

// PersonalizedFeedOptions regroupe les paramètres d'entrée du pipeline algorithmique.
type PersonalizedFeedOptions struct {
	UserID         int64          // Identifiant de l'utilisateur cible
	UserVec        []float32      // û ∈ R^224 (normalisé L2) — nil si profil vierge
	UserConfidence float64        // [0.0, 1.0] — seuil de confiance. Active le LSH si > 0.70
	FriendIDs      map[int64]bool // Set des amis directs de l'utilisateur (Pour le boost B(u,p))
	Date           time.Time      // Date de référence pour les clés ZSET quotidiennes
	Limit          int            // Taille du feed à générer (Par défaut: 50)
	CandidateIDs   []int64        // Les IDs de posts apportés par le Magasinier (Le panier brut)
	Seed           int64          // La Graine du panier pour le déterminisme
	StartIndex     int            // Index absolu de la Vague de Dopamine (Maintient la courbe au fil du scroll)
}

// buildPersonalizedFeed filtre, score et ordonne le panier brut pour créer le feed final.
func buildPersonalizedFeed(ctx context.Context, options PersonalizedFeedOptions) ([]int64, error) {
	feedSize := options.Limit
	if feedSize <= 0 {
		feedSize = variables.TDDFeedSize // K_feed = 50
	}

	// ############################################################################
	// # ÉTAPE A : VÉRIFICATION DU CACHE PRÉ-CALCULÉ
	// ############################################################################

	var cachedFeedIDs []int64
	if err := redis.FeedsPersonalized.GetObject(ctx, options.UserID, &cachedFeedIDs); err == nil && len(cachedFeedIDs) > 0 {
		return cachedFeedIDs, nil
	}

	postIDs := options.CandidateIDs
	if len(postIDs) == 0 {
		return []int64{}, nil
	}

	// ############################################################################
	// # ÉTAPE B : RÉCUPÉRATION DES SCORES DE TENDANCE MONDIAUX
	// ############################################################################

	dateKey := options.Date.UTC().Format("20060102")
	dailyTrendRedisKey := fmt.Sprintf(variables.RedisKeyTrendGlobalDaily, dateKey)

	memberStrings := make([]string, len(postIDs))
	for i, id := range postIDs {
		memberStrings[i] = strconv.FormatInt(id, 10)
	}

	// Récupération en masse des scores ZSET
	rawScores, err := redis.ZScores(ctx, dailyTrendRedisKey, memberStrings)
	if err != nil {
		numan_log.Error(ctx).Err(err).Str("dailyTrendRedisKey", dailyTrendRedisKey).Msg("buildPersonalizedFeed: Échec de la récupération des scores de tendance")
		return nil, numan_error.NewInternal()
	}

	trendScoresMap := make(map[int64]float64, len(postIDs))
	for i, id := range postIDs {
		score := rawScores[i]
		if score == 0 {
			// Si le post n'est pas dans le trend (ex: post d'un ami issu de la boîte aux lettres),
			// on lui garantit un score de base neutre pour ne pas le détruire dans la multiplication.
			score = 1.0
		}
		trendScoresMap[id] = score
	}

	// ############################################################################
	// # ÉTAPE C : HYDRATATION DES VECTEURS (CASCADE L1 -> L2 -> L3)
	// ############################################################################

	vectorBatchResult, err := redis.ContentVectors.GetMany(ctx, postIDs)
	if err != nil {
		return nil, numan_error.NewInternal()
	}

	allCandidates := make([]postCandidate, 0, len(postIDs))

	// 1. Traitement des hits du Cache L1 (RAM)
	for _, id := range postIDs {
		rawData, isFoundInL1 := vectorBatchResult.Found[id]
		if !isFoundInL1 {
			continue // Sera traité par le fallback juste après
		}

		var payload contentVectorPayload
		if err := msgpack.Unmarshal(rawData, &payload); err != nil || len(payload.Vector) != variables.VectorDimTotal {
			continue
		}

		allCandidates = append(allCandidates, postCandidate{
			PostID:        id,
			AuthorID:      payload.AuthorID,
			TrendScore:    trendScoresMap[id],
			ContentVec:    payload.Vector,
			PriorityLevel: payload.PriorityLevel,
			MatrixIdx:     len(allCandidates),
		})
	}

	// 2. AUTO-GUÉRISON : Fallback L2 (Mongo) et L3 (Postgres) pour les vecteurs disparus de la RAM
	missingIDs := vectorBatchResult.MissingIDs
	if len(missingIDs) > 0 {
		numan_log.Info(ctx).Int("missing_count", len(missingIDs)).Msg("Cache Miss sur ContentVectors, déclenchement du Fallback L2/L3...")

		// Fallback L2 (Mongo)
		mongoPosts, err := mongo.MongoLoadPosts(ctx, missingIDs)
		if err != nil {
			numan_log.Error(ctx).Err(err).Msg("buildPersonalizedFeed: Échec du chargement des posts depuis MongoDB")
			return nil, numan_error.NewInternal()
		}
		foundInMongo := make(map[int64]bool)

		for _, post := range mongoPosts {
			foundInMongo[post.ID] = true
			if len(post.Vector) == variables.VectorDimTotal {
				allCandidates = append(allCandidates, postCandidate{
					PostID:        post.ID,
					AuthorID:      post.UserID,
					TrendScore:    trendScoresMap[post.ID],
					ContentVec:    post.Vector,
					PriorityLevel: post.PriorityLevel,
					MatrixIdx:     len(allCandidates),
				})
				// Auto-guérison asynchrone du L1
				go StoreContentVector(context.Background(), post)
			}
		}

		// Fallback L3 (Postgres) pour ce qui manque toujours
		var stillMissingIDs []int64
		for _, id := range missingIDs {
			if !foundInMongo[id] {
				stillMissingIDs = append(stillMissingIDs, id)
			}
		}

		if len(stillMissingIDs) > 0 {
			pgPosts, err := postgres.FuncLoadPosts(ctx, stillMissingIDs, 1, 0)
			if err != nil {
				numan_log.Error(ctx).Err(err).Msg("buildPersonalizedFeed: Échec du chargement des posts depuis PostgreSQL")
				return nil, numan_error.NewInternal()
			}
			for _, post := range pgPosts {
				if len(post.Vector) == variables.VectorDimTotal {
					allCandidates = append(allCandidates, postCandidate{
						PostID:        post.ID,
						AuthorID:      post.UserID,
						TrendScore:    trendScoresMap[post.ID],
						ContentVec:    post.Vector,
						PriorityLevel: post.PriorityLevel,
						MatrixIdx:     len(allCandidates),
					})
					// Auto-guérison asynchrone du L1
					go StoreContentVector(context.Background(), post)
				}
			}
		}
	}

	if len(allCandidates) == 0 {
		return []int64{}, nil
	}

	// Si l'utilisateur n'a pas de profil vectoriel, on court-circuite le MMR complexe
	if len(options.UserVec) != variables.VectorDimTotal {
		return extractIDsFromCandidates(allCandidates, feedSize), nil
	}

	// ############################################################################
	// # ÉTAPE D : PRÉ-FILTRAGE LSH (Locality-Sensitive Hashing)
	// ############################################################################

	var filteredCandidates []postCandidate
	var similarityMatrix []float32
	var matrixDimension int
	var isMatrixCalculatedOnTheFly bool

	if options.UserConfidence > variables.TDDLSHConfidenceThreshold {
		lshHash := defaultLSHEngine.computeHash(options.UserVec)
		lshTargetIDSet, err := getLSHCandidateIDs(ctx, lshHash)
		if err != nil {
			numan_log.Error(ctx).Err(err).Msg("buildPersonalizedFeed: Échec de la récupération des candidats LSH")
			return nil, numan_error.NewInternal()
		}

		filtered := make([]postCandidate, 0, len(lshTargetIDSet))
		for _, candidate := range allCandidates {
			if lshTargetIDSet[candidate.PostID] {
				filtered = append(filtered, candidate)
			}
		}

		// Si le LSH a conservé assez de candidats, on l'utilise
		if len(filtered) >= feedSize*2 {
			filteredCandidates = filtered
			for i := range filteredCandidates {
				filteredCandidates[i].MatrixIdx = i
			}
			isMatrixCalculatedOnTheFly = true
		} else {
			filteredCandidates = allCandidates
		}
	} else {
		filteredCandidates = allCandidates
	}

	// ############################################################################
	// # ÉTAPE E : ÉVALUATION PERSONNALISÉE R(u,p)
	// ############################################################################

	for i := range filteredCandidates {
		filteredCandidates[i].PersonalScore = computePersonalizedScore(
			filteredCandidates[i].TrendScore,
			options.UserVec,
			filteredCandidates[i].ContentVec,
			filteredCandidates[i].AuthorID,
			options.FriendIDs,
			filteredCandidates[i].PriorityLevel,
		)
	}

	// ############################################################################
	// # ÉTAPE F : CONTRÔLE DE DIVERSITÉ (MMR) ET MATRICE
	// ############################################################################

	if isMatrixCalculatedOnTheFly {
		similarityMatrix, matrixDimension = buildSimilarityMatrix(filteredCandidates)
	} else {
		similarityMatrix, matrixDimension = getOrBuildSimMatrix(filteredCandidates)
	}

	// Exécution du Maximal Marginal Relevance
	selectedCandidates := runMMR(filteredCandidates, similarityMatrix, matrixDimension, variables.TDDLambdaMMR, feedSize)

	// ############################################################################
	// # ÉTAPE G : ONDE DE SÉRENDIPITÉ (DÉCOUVERTE)
	// ############################################################################

	serendipityDiscoveryPool := make([]int64, len(allCandidates))
	for i, candidate := range allCandidates {
		serendipityDiscoveryPool[i] = candidate.PostID
	}

	// Le générateur est ancré sur la Seed de ce panier pour garantir la stabilité de la Vague
	deterministicRNG := rand.New(rand.NewSource(options.Seed))
	selectedCandidates = injectSerendipity(selectedCandidates, serendipityDiscoveryPool, deterministicRNG, options.StartIndex)

	// Extraction de la liste finale d'IDs
	finalFeedIDs := make([]int64, len(selectedCandidates))
	for i, candidate := range selectedCandidates {
		finalFeedIDs[i] = candidate.PostID
	}

	// ############################################################################
	// # ÉTAPE H : SAUVEGARDE ET GARANTIE D'IDEMPOTENCE
	// ############################################################################

	// On fige le calcul en RAM pour intercepter les futures requêtes identiques
	err = redis.FeedsPersonalized.SetObject(ctx, options.UserID, finalFeedIDs)
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("buildPersonalizedFeed: Échec de la sauvegarde du feed personnalisé")
		return nil, numan_error.NewInternal()
	}

	return finalFeedIDs, nil
}

// InvalidatePersonalizedFeedCache détruit le cache si le vecteur de l'utilisateur a changé drastiquement.
func InvalidatePersonalizedFeedCache(ctx context.Context, userID int64, oldVector, newVector []float32) bool {
	if len(oldVector) != variables.VectorDimTotal || len(newVector) != variables.VectorDimTotal {
		return false
	}

	var sumOfSquares float64
	for i, newVelocity := range newVector {
		difference := float64(newVelocity - oldVector[i])
		sumOfSquares += difference * difference
	}

	// Calcul de l'écart géométrique Euclidien (L2)
	euclideanDelta := math.Sqrt(sumOfSquares)

	if euclideanDelta > variables.TDDDeltaInvalid {
		// Invalidation pure et atomique
		err := redis.FeedsPersonalized.DeleteObject(ctx, userID)
		if err != nil {
			numan_log.Error(ctx).Err(err).Msg("InvalidatePersonalizedFeedCache: Échec de l'invalidation du cache")
			return false
		}
		return true
	}

	return false
}

func extractIDsFromCandidates(candidates []postCandidate, limit int) []int64 {
	if limit > len(candidates) {
		limit = len(candidates)
	}
	extractedIDs := make([]int64, limit)
	for i := 0; i < limit; i++ {
		extractedIDs[i] = candidates[i].PostID
	}
	return extractedIDs
}
