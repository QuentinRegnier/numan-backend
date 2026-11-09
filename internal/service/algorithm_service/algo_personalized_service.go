package algorithm_service

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// postCandidate représente un post candidat dans le pipeline de feed personnalisé.
type postCandidate struct {
	PostID        int64
	AuthorID      int64
	TrendScore    float64   // S(p,t) — Le score de tendance global issu du ZSET Redis
	ContentVec    []float32 // ĉ_p ∈ R^224 — Le vecteur de contenu normalisé
	PriorityLevel int       // Niveau de priorité (0=Normal, 1=Certifié, 2=Admin, etc.)
	PersonalScore float64   // R(u,p) — Le score d'affinité final calculé pour CET utilisateur
	MatrixIdx     int       // Position (index) du post dans la matrice de similarité G
	IsSerendipity bool      // Détermine si le post a été injecté par le mécanisme de sérendipité
}

// simMatrixState représente l'état en mémoire de la matrice de similarité croisée.
type simMatrixState struct {
	FlattenedMatrix []float32 // Matrice aplatie [n×n] où Index = i*n + j
	DimensionSize   int       // La taille 'n' de la matrice
	PostIDs         []int64   // Liste ordonnée des IDs de posts correspondant à la matrice
	ExpiresAt       time.Time // Timestamp d'expiration du cache (habituellement 5 minutes)
}

var (
	simCacheMu sync.RWMutex
	simCache   *simMatrixState
)

// ############################################################################
// # ÉTAPE 1 : OPÉRATIONS VECTORIELLES FONDAMENTALES (SIMD FRIENDLY)
// ############################################################################

// dotProductN calcule le produit scalaire (Dot Product) de deux vecteurs.
// En Go, cette structure de boucle simple permet au compilateur d'utiliser
// les instructions matérielles SIMD (AVX2) pour accélérer le calcul massivement.
func dotProductN(vectorA, vectorB []float32) float32 {
	if len(vectorA) != len(vectorB) {
		return 0.0 // Sécurité : on évite un panic (Out of Bounds) si les vecteurs diffèrent
	}

	var dotProduct float32 = 0.0
	for i, valueA := range vectorA {
		dotProduct += valueA * vectorB[i]
	}

	return dotProduct
}

// ############################################################################
// # ÉTAPE 2 : CALCUL DES SCORES D'AFFINITÉ ET DE CORRÉLATION
// ############################################################################

// computeSocialAffinity calcule le score d'affinité sociale A(u,p).
// Il extrait uniquement le "bloc social" des vecteurs (les 64 dernières dimensions)
// et normalise le résultat du cosinus entre 0 et 1.
func computeSocialAffinity(userVec, contentVec []float32) float64 {
	if len(userVec) < variables.VectorDimTotal || len(contentVec) < variables.VectorDimTotal {
		return 0.5 // Valeur neutre si les vecteurs sont incomplets ou corrompus
	}

	// 1. Extraction des sous-blocs sociaux grâce aux variables globales
	startIndex := variables.VectorOffSoc
	endIndex := variables.VectorOffSoc + variables.VectorDimSoc

	userSocialBlock := userVec[startIndex:endIndex]
	contentSocialBlock := contentVec[startIndex:endIndex]

	// 2. Produit scalaire sur le sous-espace social (Donne une valeur entre -1 et 1)
	dot := dotProductN(userSocialBlock, contentSocialBlock)

	// 3. Normalisation Mathématique : On translate de [-1, 1] vers [0, 1]
	normalizedAffinity := (float64(dot) + 1.0) / 2.0
	return normalizedAffinity
}

// computePearsonEngagement calcule la corrélation de Pearson sur le bloc d'engagement.
// Mesure la cohérence comportementale entre ce que l'utilisateur fait, et ce que le post génère.
func computePearsonEngagement(userEngBlock, contentEngBlock []float32) float64 {
	const dimension = variables.VectorDimEng // Par défaut = 8

	// 1. Calcul des moyennes des deux blocs
	var meanUser, meanContent float64
	for i := 0; i < dimension; i++ {
		meanUser += float64(userEngBlock[i])
		meanContent += float64(contentEngBlock[i])
	}
	meanUser /= float64(dimension)
	meanContent /= float64(dimension)

	// 2. Calcul des écarts (Covariance) et des variances au carré
	var sumCovariance, sumUserVarianceSq, sumContentVarianceSq float64
	for i := 0; i < dimension; i++ {
		deltaUser := float64(userEngBlock[i]) - meanUser
		deltaContent := float64(contentEngBlock[i]) - meanContent

		sumCovariance += deltaUser * deltaContent
		sumUserVarianceSq += deltaUser * deltaUser
		sumContentVarianceSq += deltaContent * deltaContent
	}

	// 3. Dénominateur : Racine de la multiplication des variances
	denominator := math.Sqrt(sumUserVarianceSq * sumContentVarianceSq)

	// Sécurité anti-division par zéro (Si l'un des vecteurs est plat/constant)
	if denominator < 1e-10 {
		return 0.0 // Corrélation indéfinie = neutre
	}

	return sumCovariance / denominator
}

// computePersonalizedScore assemble toutes les métriques pour fournir le score R(u,p).
// C'est le score final qui décidera si le post mérite d'apparaître pour cet utilisateur.
func computePersonalizedScore(
	trendScore float64,
	userVec, contentVec []float32,
	authorID int64,
	friendIDs map[int64]bool,
	priorityLevel int,
) float64 {

	// Si les vecteurs sont invalides, on fallback sur le score global influencé par la priorité
	if len(userVec) != variables.VectorDimTotal || len(contentVec) != variables.VectorDimTotal {
		return trendScore * (1.0 + float64(priorityLevel)*0.5)
	}

	// ÉTAPE A : Similarité Cosinus Globale (Produit scalaire complet)
	cosineSimilarity := float64(dotProductN(userVec, contentVec))

	// ÉTAPE B : Affinité Sociale pure (A(u,p))
	socialAffinity := computeSocialAffinity(userVec, contentVec)

	// ÉTAPE C : Indicateur d'amitié directe (B(u,p))
	var friendBoost = 0.0
	if friendIDs != nil && friendIDs[authorID] {
		friendBoost = 1.0
	}

	// ÉTAPE D : Cohérence comportementale (Pearson sur le bloc engagement)
	startIndex := variables.VectorOffEng
	endIndex := variables.VectorOffEng + variables.VectorDimEng
	pearsonCorrelation := computePearsonEngagement(userVec[startIndex:endIndex], contentVec[startIndex:endIndex])

	// ÉTAPE E : Formule Composite (La recette secrète numan)
	// Base : (ρ * Cosinus) + ((1 - ρ) * Affinité Sociale) + (η * Ami) + (η_P * Pearson)
	innerFormula := (variables.TDDRho * cosineSimilarity) +
		((1.0 - variables.TDDRho) * socialAffinity) +
		(variables.TDDEta * friendBoost) +
		(variables.TDDEtaP * pearsonCorrelation)

	baseScore := trendScore * innerFormula

	// ÉTAPE F : Multiplicateur de Priorité (Admin, Certifié, etc.)
	priorityMultiplier := 1.0 + (float64(priorityLevel) * 0.5)

	return baseScore * priorityMultiplier
}

// ############################################################################
// # ÉTAPE 3 : MATRICE DE SIMILARITÉ ET CACHE EN MÉMOIRE
// ############################################################################

// buildSimilarityMatrix construit la matrice de similarité croisée G.
// G_{i,j} = Similarité entre le post_service I et le post_service J.
// Elle est stockée sous forme de tableau plat (row-major) pour la performance CPU.
func buildSimilarityMatrix(candidates []postCandidate) ([]float32, int) {
	dimensionSize := len(candidates)
	if dimensionSize == 0 {
		return nil, 0
	}

	// Allocation de la matrice aplatie
	flattenedMatrix := make([]float32, dimensionSize*dimensionSize)

	for i := 0; i < dimensionSize; i++ {
		// La similarité d'un post avec lui-même est toujours 1.0 (Vecteurs normalisés L2)
		flattenedMatrix[i*dimensionSize+i] = 1.0

		vectorI := candidates[i].ContentVec
		if len(vectorI) != variables.VectorDimTotal {
			continue
		}

		// Optimisation mathématique : la matrice est symétrique (G[i,j] == G[j,i]).
		// On ne calcule que le triangle supérieur.
		for j := i + 1; j < dimensionSize; j++ {
			vectorJ := candidates[j].ContentVec
			if len(vectorJ) != variables.VectorDimTotal {
				continue
			}

			dot := dotProductN(vectorI, vectorJ)
			flattenedMatrix[i*dimensionSize+j] = dot
			flattenedMatrix[j*dimensionSize+i] = dot
		}
	}

	return flattenedMatrix, dimensionSize
}

// getOrBuildSimMatrix retourne la matrice depuis la RAM, ou la calcule si le cache a expiré.
func getOrBuildSimMatrix(candidates []postCandidate) ([]float32, int) {
	now := domain.NowTime()

	// 1. Lecture Rapide (RLock)
	simCacheMu.RLock()
	isCacheValid := simCache != nil && now.Before(simCache.ExpiresAt)

	if isCacheValid && candidatesMatch(simCache.PostIDs, candidates) {
		matrix, size := simCache.FlattenedMatrix, simCache.DimensionSize
		simCacheMu.RUnlock()
		return matrix, size
	}
	simCacheMu.RUnlock()

	// 2. Reconstruction car le cache est invalide ou les candidats ont changé
	newMatrix, newSize := buildSimilarityMatrix(candidates)

	extractedPostIDs := make([]int64, len(candidates))
	for i, candidate := range candidates {
		extractedPostIDs[i] = candidate.PostID
	}

	// 3. Écriture Sécurisée (Lock exclusif avec Double-Check Pattern)
	simCacheMu.Lock()
	defer simCacheMu.Unlock()

	isCacheValidAfterLock := simCache != nil && now.Before(simCache.ExpiresAt)
	if isCacheValidAfterLock && candidatesMatch(simCache.PostIDs, candidates) {
		return simCache.FlattenedMatrix, simCache.DimensionSize
	}

	simCache = &simMatrixState{
		FlattenedMatrix: newMatrix,
		DimensionSize:   newSize,
		PostIDs:         extractedPostIDs,
		ExpiresAt:       now.Add(5 * time.Minute), // Renouvellement de la fenêtre de 5 min
	}

	return newMatrix, newSize
}

// candidatesMatch vérifie en O(n) si la liste stockée en cache correspond aux candidats actuels.
func candidatesMatch(cachedIDs []int64, candidates []postCandidate) bool {
	if len(cachedIDs) != len(candidates) {
		return false
	}
	for i, candidate := range candidates {
		if cachedIDs[i] != candidate.PostID {
			return false
		}
	}
	return true
}

// ############################################################################
// # ÉTAPE 4 : MOTEUR DE DIVERSITÉ MMR (Maximal Marginal Relevance)
// ############################################################################

// runMMR sélectionne les posts en équilibrant deux forces :
// 1. Pertinence (Le post est-il parfait pour l'utilisateur ?)
// 2. Redondance (L'utilisateur vient-il de voir 5 posts identiques juste avant ?)
func runMMR(candidates []postCandidate, similarityMatrix []float32, matrixDimension int, lambdaDiversity float64, requestedLimit int) []postCandidate {
	totalCandidates := len(candidates)
	if totalCandidates == 0 || requestedLimit <= 0 {
		return nil
	}

	if requestedLimit > totalCandidates {
		requestedLimit = totalCandidates
	}

	selectedCandidates := make([]postCandidate, 0, requestedLimit)
	selectedMatrixIndexes := make([]int, 0, requestedLimit)

	isAvailable := make([]bool, totalCandidates)
	for i := range isAvailable {
		isAvailable[i] = true
	}

	// Boucle principale : on tire les posts 1 par 1 jusqu'à atteindre la limite demandée
	for len(selectedCandidates) < requestedLimit {
		var bestMarginalScore = math.Inf(-1)
		bestCandidateIndex := -1

		// Évaluation marginale de tous les candidats restants
		for i := range candidates {
			if !isAvailable[i] {
				continue
			}

			// Force A : La pertinence (pondérée par Lambda)
			relevanceScore := lambdaDiversity * candidates[i].PersonalScore
			marginalScore := relevanceScore

			// Force B : La pénalité de redondance
			// Si on a déjà sélectionné des posts, on vérifie à quel point ce candidat
			// ressemble au pire de ce qu'on a déjà pris.
			if len(selectedMatrixIndexes) > 0 {
				var maxSimilarityWithSelected = 0.0
				candidateMatrixIdx := candidates[i].MatrixIdx

				for _, selectedIdx := range selectedMatrixIndexes {
					targetMatrixIdx := candidates[selectedIdx].MatrixIdx

					// Extraction dans la matrice plate : G[ligne * taille + colonne]
					if candidateMatrixIdx >= 0 && targetMatrixIdx >= 0 && (candidateMatrixIdx*matrixDimension+targetMatrixIdx) < len(similarityMatrix) {
						similarity := float64(similarityMatrix[candidateMatrixIdx*matrixDimension+targetMatrixIdx])
						if similarity > maxSimilarityWithSelected {
							maxSimilarityWithSelected = similarity
						}
					}
				}

				redundancyPenalty := (1.0 - lambdaDiversity) * maxSimilarityWithSelected
				marginalScore -= redundancyPenalty
			}

			// Retenir le candidat ayant le meilleur score net (Pertinence - Redondance)
			if marginalScore > bestMarginalScore {
				bestMarginalScore = marginalScore
				bestCandidateIndex = i
			}
		}

		// Rupture si on n'a plus rien à évaluer
		if bestCandidateIndex < 0 {
			break
		}

		// Validation et verrouillage du candidat élu
		selectedCandidates = append(selectedCandidates, candidates[bestCandidateIndex])
		selectedMatrixIndexes = append(selectedMatrixIndexes, bestCandidateIndex)
		isAvailable[bestCandidateIndex] = false
	}

	return selectedCandidates
}

// ############################################################################
// # ÉTAPE 5 : INJECTION DE SÉRENDIPITÉ (ONDE DE DOPAMINE)
// ############################################################################

// injectSerendipity remplace de manière aléatoire (mais contrôlée) certains posts
// du feed généré par MMR par des posts de découverte pour casser les chambres d'écho.
func injectSerendipity(feed []postCandidate, discoveryPool []int64, rng *rand.Rand, startIndex int) []postCandidate {
	if len(discoveryPool) == 0 || rng == nil {
		return feed
	}

	for i := range feed {
		// 1. L'Onde de Dopamine (DopamineWave) dicte combien de contenu "sûr"
		// l'utilisateur a besoin à cet instant précis (index).
		affinityRequired := dopamineWave(float64(startIndex + i))

		// 2. La probabilité de surprise (Sérendipité) est l'inverse exact de ce besoin.
		// Ex: Si le besoin de certitude est de 0.8 (80%), la probabilité de surprise est 20%.
		probabilityOfSurprise := 1.0 - affinityRequired

		// 3. Jet de dé contre la probabilité calculée
		if rng.Float64() < probabilityOfSurprise {
			randomPoolIndex := rng.Intn(len(discoveryPool))

			// On écrase le post du MMR par un post de découverte brut
			feed[i] = postCandidate{
				PostID:        discoveryPool[randomPoolIndex],
				IsSerendipity: true,
				MatrixIdx:     -1, // Sécurité : ce post n'est pas dans la matrice calculée
			}
		}
	}

	return feed
}
