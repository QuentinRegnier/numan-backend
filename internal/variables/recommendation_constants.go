package variables

import "time"

// ============================================================================
// INVARIANTS STRUCTURELS LSH & BUFFER AU BOOT (IMMUABLES)
// ============================================================================

const (
	TDDLSHBits         = 32        // b — bits de projection aléatoire (SimHash)
	TDDLSHSeed         = int64(42) // Graine LSH (ne doit pas changer sans reconstruire les buckets)
	TimeDecayJobBuffer = 10000     // Taille du channel allouée au démarrage du Time-Decay Engine
)

// ============================================================================
// PARAMÈTRES DYNAMIQUES — ALGORITHMES DE RECOMMANDATION ("TABLE DE MIXAGE")
// ============================================================================

var (
	// --- PILIER 2 : SCORE DE TENDANCE S(p,t) ---
	TDDWeightLike    = 1.0 // w_like
	TDDWeightComment = 1.5 // w_comment
	TDDWeightMedia   = 0.3 // w_media — bonus de richesse du contenu

	TDDAlpha = 0.85 // α — exposant sublinéaire (amortit la dominance virale, loi de Zipf)
	TDDBeta  = 1.60 // β — exposant du déclin temporel polynomial
	TDDTheta = 1.80 // θ — constante de gravité (108 min de période de grâce initiale)

	TDDLambdaDecay = 0.04 // λ — taux de déclin exponentiel post-grâce
	TDDTGrace      = 72.0 // T_grace (h) — 72 heures de déclin purement polynomial

	TDDPhiMedia = 0.25 // φ_media — bonus média (+25%)
	TDDPhiGrade = 0.15 // φ_grade — bonus grade auteur
	TDDGradeMax = 3.0  // g_max — grade maximum (modérateur)

	TDDGammaAuth = 0.50 // γ_auth — taux de pénalité diversité auteur

	// --- PILIER 3 : SCORE PERSONNALISÉ R(u,p) ---
	TDDRho  = 0.65 // ρ — poids composante vectorielle vs sociale
	TDDEta  = 0.20 // η — boost amis directs
	TDDEtaP = 0.10 // η_p — poids corrélation de Pearson engagement

	TDDLambdaMMR              = 0.72 // λ_MMR — paramètre de diversité MMR
	TDDDeltaInvalid           = 0.15 // δ_inval — seuil d'invalidation du cache feed
	TDDLSHConfidenceThreshold = 0.70

	// --- WORKER DE TENDANCES (TRENDS) ---
	TrendCronInterval        = 15 * time.Minute // Fréquence d'actualisation des tendances
	TrendTopPostsLimit int64 = 1000             // Volume de posts analysés pour dégager une tendance
	TrendTagsRetention       = -101             // Rétention ZSET (Garde les 100 premiers, purge le reste)

	// --- WORKER DE CANONICALISATION DES TAGS ---
	HashtagCanonCronInterval = 24 * time.Hour // Fréquence d'exécution du Job (Nuit)
	HashtagCanonMinLength    = 4              // Longueur minimum pour éviter les faux positifs
	HashtagCanonMaxDistance  = 0.15           // Seuil TDD de Levenshtein (Tolérance de 15% de différence)
)
