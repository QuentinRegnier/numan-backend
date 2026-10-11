package algorithm_service

import (
	"context"
	"math"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # PILIER 3 : VECTORISATION DU CONTENU CÔTÉ SERVEUR
// ############################################################################

// contentVectorPayload est la structure sérialisée dans Redis sous content:vec:{post_id}.
type contentVectorPayload struct {
	Vector        []float32 `json:"v"`         // ĉ_p ∈ R^224 (normalisé L2)
	LSHHash       uint32    `json:"lsh"`       // Hash LSH pré-calculé pour le bucket
	AuthorID      int64     `json:"author_id"` // Pour le calcul de B(u,p)
	PriorityLevel int       `json:"priority"`  // Multiplicateur pour la Caissière
}

// ContentVectorOptions permet d'injecter les embeddings externes optionnels.
type ContentVectorOptions struct {
	TagEmbeddings  map[string][]float32
	AuthorSocEmbed []float32
}

// ############################################################################
// # ENTRÉE PUBLIQUE : GÉNÉRATION ET SAUVEGARDE
// ############################################################################

// StoreContentVector calcule et stocke de manière asynchrone le vecteur ĉ_p du post.
func StoreContentVector(ctx context.Context, post post_models.PostPayload) {
	fullVector := ComputeContentVectorFull(ctx, post, nil)

	payload := contentVectorPayload{
		Vector:        fullVector,
		LSHHash:       defaultLSHEngine.computeHash(fullVector),
		AuthorID:      post.UserID,
		PriorityLevel: post.PriorityLevel,
	}

	if err := redis.ContentVectors.SetObject(ctx, post.ID, payload); err != nil {
		numan_log.Error(ctx).Err(err).Int64("post_id", post.ID).Msg("Échec Redis SET content:vec via Collection")
		return
	}

	if err := storeLSHBucket(ctx, post.ID, payload.LSHHash); err != nil {
		numan_log.Error(ctx).Err(err).Int64("post_id", post.ID).Msg("Échec mise à jour Redis LSH bucket")
	}
}

// UpdatePostEngagementVector met à jour de manière asynchrone le bloc engagement.
func UpdatePostEngagementVector(ctx context.Context, post post_models.PostPayload) {
	var payload contentVectorPayload

	// 1. TENTATIVE L1 : Récupération du vecteur actuel
	if err := redis.ContentVectors.GetObject(ctx, post.ID, &payload); err != nil || len(payload.Vector) != variables.VectorDimTotal {
		numan_log.Warn(ctx).Err(err).Int64("post_id", post.ID).Msg("Vecteur absent ou corrompu en RAM, recalcul complet déclenché")
		// FALLBACK : Recalcul complet si le payload est absent (Auto-guérison par calcul)
		StoreContentVector(ctx, post)
		return
	}

	// 2. Mise à jour uniquement du bloc engagement [152:160)
	engagementStartIndex := variables.VectorOffEng
	engagementEndIndex := variables.VectorOffEng + variables.VectorDimEng
	engagementBlock := payload.Vector[engagementStartIndex:engagementEndIndex]

	computeEngagementBlock(post, engagementBlock)

	// 3. Re-normalisation L2 après modification du bloc
	normalizeL2(payload.Vector)

	// 4. Mise à jour du hash LSH après re-normalisation
	payload.LSHHash = defaultLSHEngine.computeHash(payload.Vector)

	// 5. Sauvegarde atomique L1
	if err := redis.ContentVectors.SetObject(ctx, post.ID, payload); err != nil {
		numan_log.Error(ctx).Err(err).Int64("post_id", post.ID).Msg("Échec de la mise à jour asynchrone du vecteur")
	}
}

// ############################################################################
// # IMPLÉMENTATION INTERNE : CONSTRUCTION DES BLOCS
// ############################################################################

// ComputeContentVectorFull construit le vecteur complet ĉ_p ∈ R^224 normalisé.
func ComputeContentVectorFull(ctx context.Context, post post_models.PostPayload, options *ContentVectorOptions) []float32 {
	fullVector := make([]float32, variables.VectorDimTotal)

	categoricalBlock := fullVector[variables.VectorOffCat : variables.VectorOffCat+variables.VectorDimCat]
	temporalBlock := fullVector[variables.VectorOffTemp : variables.VectorOffTemp+variables.VectorDimTemp]
	engagementBlock := fullVector[variables.VectorOffEng : variables.VectorOffEng+variables.VectorDimEng]
	socialBlock := fullVector[variables.VectorOffSoc : variables.VectorOffSoc+variables.VectorDimSoc]

	// BLOC 1 : Catégoriel
	if options != nil && len(options.TagEmbeddings) > 0 {
		computeCategoricalBlock(ctx, post.Hashtags, options.TagEmbeddings, categoricalBlock)
	}

	// BLOC 2 : Temporel
	computeTemporalBlock(domain.MillisToTime(post.CreatedAt).Hour(), temporalBlock)

	// BLOC 3 : Engagement
	computeEngagementBlock(post, engagementBlock)

	// BLOC 4 : Social
	if options != nil && len(options.AuthorSocEmbed) >= variables.VectorDimSoc {
		computeSocialBlock(options.AuthorSocEmbed, socialBlock)
	}

	// Normalisation L2 finale pour garantir <ĉ_p, û> ≡ cos(c_p, u)
	normalizeL2(fullVector)

	return fullVector
}

// computeCategoricalBlock calcule le bloc catégoriel c_p^(cat) ∈ R^128.
func computeCategoricalBlock(ctx context.Context, hashtags []string, embeddings map[string][]float32, targetBlock []float32) {
	for _, tag := range hashtags {
		normalizedTag := service.NormalizeHashtag(ctx, tag)
		embeddingVector, exists := embeddings[normalizedTag]

		if !exists || len(embeddingVector) < variables.VectorDimCat {
			continue
		}

		for k := 0; k < variables.VectorDimCat; k++ {
			targetBlock[k] += embeddingVector[k]
		}
	}

	divisor := float32(len(hashtags) + 1)
	if divisor > 0 {
		inverseDivisor := float32(1.0) / divisor
		for k := range targetBlock {
			targetBlock[k] *= inverseDivisor
		}
	}
}

// computeTemporalBlock calcule le bloc temporel c_p^(temp) ∈ R^24.
func computeTemporalBlock(publicationHour int, targetBlock []float32) {
	const varianceDenominator = 2.0 * variables.TDDSigmaHours * variables.TDDSigmaHours

	var sumProbabilities float64
	for hourIndex := 0; hourIndex < 24; hourIndex++ {
		difference := float64(hourIndex - publicationHour)
		gaussianValue := math.Exp(-(difference * difference) / varianceDenominator)
		targetBlock[hourIndex] = float32(gaussianValue)
		sumProbabilities += gaussianValue
	}

	if sumProbabilities > 1e-12 {
		inverseProbabilitySum := float32(1.0 / sumProbabilities)
		for hourIndex := 0; hourIndex < 24; hourIndex++ {
			targetBlock[hourIndex] *= inverseProbabilitySum
		}
	}
}

// computeEngagementBlock calcule le bloc engagement c_p^(eng) ∈ R^8.
func computeEngagementBlock(post post_models.PostPayload, targetBlock []float32) {
	applySigmoid := func(x float64) float32 {
		return float32(1.0 / (1.0 + math.Exp(-x)))
	}

	views := math.Max(1.0, float64(post.ViewCount))
	likes := math.Max(0.0, float64(post.LikeCount))
	comments := math.Max(0.0, float64(post.CommentCount))

	targetBlock[0] = 0.0                                                 // τ̄_dwell (zéro à la création)
	targetBlock[1] = 0.0                                                 // σ_τ (zéro à la création)
	targetBlock[2] = applySigmoid(likes / views * 10.0)                  // r_like
	targetBlock[3] = applySigmoid(comments / math.Max(1.0, likes) * 5.0) // r_comment
	targetBlock[4] = 0.0                                                 // r_scroll_deep
	targetBlock[5] = 0.0                                                 // r_profile_visit

	mediaCount := float64(len(post.MediaIDs))
	targetBlock[6] = float32(math.Min(1.0, mediaCount/5.0)) // n̄_session proxy

	if post.HasMedia {
		targetBlock[7] = 0.5 // d̄_session proxy
	} else {
		targetBlock[7] = 0.0
	}
}

// computeSocialBlock copie l'embedding social de l'auteur.
func computeSocialBlock(authorSocialEmbedding []float32, targetBlock []float32) {
	dimensionSize := variables.VectorDimSoc
	if len(authorSocialEmbedding) < dimensionSize {
		dimensionSize = len(authorSocialEmbedding)
	}
	copy(targetBlock[:dimensionSize], authorSocialEmbedding[:dimensionSize])
}

// normalizeL2 normalise le vecteur à la norme unitaire (in-place).
func normalizeL2(vectorToNormalize []float32) {
	var squaredNormSum float64
	for _, value := range vectorToNormalize {
		squaredNormSum += float64(value) * float64(value)
	}

	if squaredNormSum < 1e-12 {
		return // Vecteur quasi-nul
	}

	inverseNorm := float32(1.0 / math.Sqrt(squaredNormSum))
	for i := range vectorToNormalize {
		vectorToNormalize[i] *= inverseNorm
	}
}
