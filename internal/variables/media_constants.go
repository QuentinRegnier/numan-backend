package variables

// ============================================================================
// ÉTATS DE VISIBILITÉ DES MÉDIAS (ÉNUMÉRATIONS BDD IMMUABLES)
// ============================================================================

const (
	MediaVisibilityRejected    = -2 // Contenu illicite rejeté par un modérateur (purgé par GC)
	MediaVisibilitySoftDeleted = -1 // Média rattaché à un post/message supprimé (purgé après 48h)
	MediaVisibilityPending     = 0  // Coquille uploadée sur S3 en attente d'attachement (purgée après 6h)
	MediaVisibilityPublic      = 1  // Média actif, validé et chargeable via URL signée HMAC
	MediaVisibilityQuarantine  = 2  // Média uploadé sur S3 mais bloqué en quarantaine (attente modérateur)
)

// ============================================================================
// PARAMÈTRES DYNAMIQUES DU DOMAINE MÉDIA
// ============================================================================

var (
	// Limites de résolution pour les uploads d'images
	MediaMaxPixels = 2000 * 2000 // Limite de surface (4 Mégapixels)
	MediaMaxWidth  = 1920        // Largeur maximale avant redimensionnement (Lanczos)

	// Paramètres d'encodage AVIF
	MediaAvifQuality = 65 // Qualité de compression (1-100)
	MediaAvifSpeed   = 5  // Vitesse d'encodage (0-10)
)
