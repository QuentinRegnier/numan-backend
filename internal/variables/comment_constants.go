package variables

// ============================================================================
// PARAMÈTRES ET CONSTANTES DU DOMAINE COMMENTAIRES
// ============================================================================

const (
	// État BDD immuable (Soft Delete)
	CommentVisibilitySoftDelete = -1
)

var (
	// CommentPriorityMultiplier est le coefficient appliqué au grade de l'auteur (0 à 4)
	// pour calculer le score initial d'affichage dans le ZSET des commentaires (L1).
	CommentPriorityMultiplier = 10000
)
