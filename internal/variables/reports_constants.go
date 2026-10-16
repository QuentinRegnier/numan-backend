package variables

// ############################################################################
// # CONSTANTES : CIBLES, CATÉGORIES ET ÉTATS DE SIGNALEMENT (IMMUABLES)
// ############################################################################

const (
	ReportTargetPost         = 0
	ReportTargetComment      = 1
	ReportTargetConversation = 2
	ReportTargetMessage      = 3
	ReportTargetUser         = 5

	ReportCatSpam             = 1
	ReportCatHarassmentMoral  = 2  // Harcèlement moral / Cyberintimidation
	ReportCatHarassmentSexual = 3  // Harcèlement sexuel
	ReportCatHateSpeech       = 4  // Incitation à la haine / Violence
	ReportCatIdentityTheft    = 5  // Usurpation d'identité
	ReportCatUnderage         = 6  // Présence d'un mineur
	ReportCatNonConsensual    = 7  // Partage de contenu non consenti
	ReportCatIllegalContent   = 8  // Contenu illégal (Drogue, Armes, etc.)
	ReportCatSelfHarm         = 9  // Automutilation / Suicide
	ReportCatOther            = 99 // Autre (se référer au champ "reason")

	ReportStatePending    = 0  // En attente de traitement
	ReportStateInProgress = 1  // En cours de traitement par un modérateur
	ReportStateEscalated  = 2  // Escaladé à un supérieur
	ReportStateClosed     = -1 // Fermé / Traité
)

// ############################################################################
// # PARAMÈTRES DYNAMIQUES : CALCULATEUR ÉCONOMIQUE & LIMITES
// ############################################################################

var (
	// Utilisateur
	ReportScoreFollowerMultiplier = 1.0
	ReportScoreCertifiedBonus     = 5000.0
	ReportScorePartnerBonus       = 20000.0
	ReportScoreStaffBonus         = 50000.0

	// Publication (Post)
	ReportScoreViewMultiplier    = 0.1
	ReportScoreLikeMultiplier    = 2.0
	ReportScoreCommentMultiplier = 5.0
	ReportScoreDwellMultiplier   = 0.5

	// Commentaire
	ReportScoreCommentLikeMultiplier = 1.5

	// Quota par requête
	MaxReportTargets = 20
)
