package variables

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
