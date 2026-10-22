package algorithm_service

import (
	"context"
	"math/rand"
	"strconv"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # PILIER 3 : APPROXIMATION PAR LOCALITÉ (LSH)
// ############################################################################

// lshEngine encapsule la matrice de projection aléatoire P ∈ R^{b×N}
type lshEngine struct {
	projectionMatrix []float32
	hashBits         int // Par défaut : 32
	vectorDimension  int // Par défaut : 224
}

// defaultLSHEngine est l'instance singleton partagée par tous les workers Go.
var defaultLSHEngine *lshEngine

func init() {
	defaultLSHEngine = newLSHEngine(variables.TDDLSHSeed)
}

// newLSHEngine crée un LSHEngine avec une matrice de projection aléatoire.
func newLSHEngine(seed int64) *lshEngine {
	bitsCount := variables.TDDLSHBits
	dimensionSize := variables.VectorDimTotal

	randomGen := rand.New(rand.NewSource(seed))
	matrixSize := bitsCount * dimensionSize
	generatedMatrix := make([]float32, matrixSize)

	for i := range generatedMatrix {
		generatedMatrix[i] = float32(randomGen.NormFloat64())
	}

	return &lshEngine{
		projectionMatrix: generatedMatrix,
		hashBits:         bitsCount,
		vectorDimension:  dimensionSize,
	}
}

// ############################################################################
// # CALCUL DU HASH ET OPÉRATIONS LSH
// ############################################################################

// computeHash calcule le hash LSH d'un vecteur v ∈ R^N.
func (engine *lshEngine) computeHash(vector []float32) uint32 {
	if len(vector) < engine.vectorDimension {
		return 0
	}

	var computedHash uint32

	for bitIndex := 0; bitIndex < engine.hashBits; bitIndex++ {
		baseIndex := bitIndex * engine.vectorDimension

		var dotProduct float32
		matrixRow := engine.projectionMatrix[baseIndex : baseIndex+engine.vectorDimension]

		for j, projectionValue := range matrixRow {
			dotProduct += projectionValue * vector[j]
		}

		if dotProduct > 0 {
			computedHash |= 1 << uint(bitIndex)
		}
	}

	return computedHash
}

// neighborHashes retourne les hashes voisins à distance de Hamming ≤ 1.
func (engine *lshEngine) neighborHashes(targetHash uint32) []uint32 {
	neighborList := make([]uint32, 0, engine.hashBits+1)
	neighborList = append(neighborList, targetHash) // Bucket exact

	for bitIndex := 0; bitIndex < engine.hashBits; bitIndex++ {
		neighborList = append(neighborList, targetHash^(1<<uint(bitIndex)))
	}

	return neighborList
}

// ############################################################################
// # INTERACTIONS REDIS (GESTION DES BUCKETS LSH)
// ############################################################################

// storeLSHBucket enregistre un post dans son bucket LSH Redis.
func storeLSHBucket(ctx context.Context, postID int64, hashValue uint32) error {
	memberIDString := strconv.FormatInt(postID, 10)

	if err := redis.LSHBuckets.SAdd(ctx, hashValue, memberIDString); err != nil {
		return err
	}

	err := redis.LSHBuckets.RefreshTTL(ctx, hashValue)
	if err != nil {
		return err
	}
	return nil
}

// getLSHCandidateIDs récupère l'ensemble des IDs présents dans les buckets voisins.
func getLSHCandidateIDs(ctx context.Context, targetHash uint32) (map[int64]bool, error) {
	neighborHashes := defaultLSHEngine.neighborHashes(targetHash)
	candidateSet := make(map[int64]bool, 200)

	for _, hashValue := range neighborHashes {
		memberIDs, err := redis.LSHBuckets.SMembers(ctx, hashValue)
		if err != nil {
			numan_log.Warn(ctx).Err(err).Uint32("bucket", hashValue).Msg("Lookup LSH bucket ignoré")
			continue
		}
		parsedIDs := pkg.ParseInt64List(memberIDs)
		for _, parsedID := range parsedIDs {
			candidateSet[parsedID] = true
		}
	}

	return candidateSet, nil
}

// removeLSHBucket retire un post de son bucket LSH.
func removeLSHBucket(ctx context.Context, postID int64, hashValue uint32) error {
	return redis.LSHBuckets.SRem(ctx, hashValue, strconv.FormatInt(postID, 10))
}

// PurgePostVectors supprime le vecteur d'engagement du post et le retire de son bucket LSH.
func PurgePostVectors(ctx context.Context, postID int64) error {
	var payload contentVectorPayload

	if err := redis.ContentVectors.GetObject(ctx, postID, &payload); err == nil {
		err = removeLSHBucket(ctx, postID, payload.LSHHash)
		if err != nil {
			numan_log.Warn(ctx).Err(err).Int64("post_id", postID).Msg("Impossible de retirer le post de son bucket LSH")
			return err
		}
	}

	return redis.ContentVectors.DeleteObject(ctx, postID)
}
