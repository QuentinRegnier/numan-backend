package service

import (
	"math"

	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # PILIER 2 : ALGORITHME DE RECOMMANDATION GLOBAL (DÉTECTION DES TENDANCES)
// ############################################################################

// ScoreOptions contient les métriques brutes nécessaires au calcul de S(p,t).
type ScoreOptions struct {
	// ── Signaux d'engagement ──────────────────────────────────────────────
	LikesCount    int // Le nombre total de likes
	CommentsCount int // Le nombre total de commentaires
	ViewCount     int // Le nombre total de vues
	MediaCount    int // Le nombre de médias attachés au post

	// ── Contexte auteur ───────────────────────────────────────────────────
	AuthorGrade         int // Niveau de confiance de l'auteur (0=Normal, 1=Certifié, etc.)
	AuthorPostsInWindow int // Nombre de posts du même auteur dans la fenêtre temporelle actuelle

	// ── Contexte temporel ─────────────────────────────────────────────────
	AgeSeconds float64 // Âge du post en secondes depuis sa création

	// ── Facteur de modération ─────────────────────────────────────────────
	IsDeleted   bool // True si le post a été supprimé
	ReportCount int  // Nombre de signalements par les utilisateurs
}

// CalculateRecommendationScore calcule S(p, t) — le score de tendance global.
// C'est ce score qui détermine si un post devient viral mondialement.
//
// Formule composite complète :
// S(p, t) = BaseScore(p, t) · ExponentialDecay(p, t) · QualityFactor(p) · DiversityFactor(p)
func CalculateRecommendationScore(options ScoreOptions) float64 {

	// 1. FACTEUR DE MODÉRATION (phi_mod)
	if options.IsDeleted {
		return 0.0
	}

	var moderationPenalty = 1.0

	// PROTECTION ANTI-BRIGADING : Seuil absolu (ex: min 10 signalements) + Ratio d'engagement
	if options.ReportCount >= 10 {
		// Le ratio de signalement doit dépasser 1% des vues (avec un plancher à 100 vues pour lisser)
		reportRatio := float64(options.ReportCount) / math.Max(100.0, float64(options.ViewCount))
		if reportRatio > 0.01 {
			moderationPenalty = variables.TDDPhiReported // Applique la pénalité (ex: 0.5)
		}
	}

	// 2. SOMME PONDÉRÉE DE L'ENGAGEMENT
	weightedEngagementSum := (variables.TDDWeightLike * float64(options.LikesCount)) +
		(variables.TDDWeightComment * float64(options.CommentsCount)) +
		(variables.TDDWeightView * float64(options.ViewCount)) +
		(variables.TDDWeightMedia * float64(options.MediaCount))

	if weightedEngagementSum <= 0 {
		return 0.0
	}

	// 3. SCORE DE BASE (S_base) : Déclin polynomial selon la Loi de Zipf
	ageInHours := math.Max(0.0, options.AgeSeconds/3600.0)
	baseNumerator := math.Pow(weightedEngagementSum, variables.TDDAlpha)
	baseDenominator := math.Pow(ageInHours+variables.TDDTheta, variables.TDDBeta)
	baseScore := baseNumerator / baseDenominator

	// 4. DÉCLIN EXPONENTIEL (D_exp) : Période post-grâce
	var exponentialDecay = 1.0
	if ageInHours > variables.TDDTGrace {
		exponentialDecay = math.Exp(-variables.TDDLambdaDecay * (ageInHours - variables.TDDTGrace))
	}

	// 5. FACTEUR DE QUALITÉ (Phi) : Richesse du contenu et statut de l'auteur
	var mediaBonus = 1.0
	if options.MediaCount > 0 {
		mediaBonus = 1.0 + variables.TDDPhiMedia
	}

	normalizedAuthorGrade := math.Min(1.0, float64(options.AuthorGrade)/variables.TDDGradeMax)
	authorBonus := 1.0 + (variables.TDDPhiGrade * normalizedAuthorGrade)

	qualityFactor := mediaBonus * authorBonus * moderationPenalty

	// 6. FACTEUR DE DIVERSITÉ (V) : Empêche un même auteur d'inonder le feed
	diversityFactor := math.Exp(-variables.TDDGammaAuth * float64(options.AuthorPostsInWindow))

	// FORMULE FINALE
	return baseScore * exponentialDecay * qualityFactor * diversityFactor
}

// ############################################################################
// # CALCUL DES TENDANCES DE HASHTAGS
// ############################################################################

// ComputeHashtagTrendScore calcule T(h, t) — le score de tendance d'un hashtag canonique.
// Il additionne les scores de tendance de tous les posts récents contenant ce hashtag.
func ComputeHashtagTrendScore(postScoresMap map[int64]float64, postAgesMap map[int64]float64) float64 {
	maxAllowedAgeInSeconds := variables.TDDHashtagWindowHours * 3600.0
	var totalTrendScore = 0.0

	for postID, currentScore := range postScoresMap {
		ageInSeconds, isAgeKnown := postAgesMap[postID]

		// Filtre Temporel : On n'additionne que si le post est dans la fenêtre autorisée (ex: 48h)
		if isAgeKnown && ageInSeconds <= maxAllowedAgeInSeconds {
			totalTrendScore += currentScore
		}
	}

	return totalTrendScore
}
