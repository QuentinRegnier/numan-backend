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
	ReportCatAIPornography    = 10 // Détection automatique IA NSFWJS (Classe Porn)

	// Origine du signalement (Utilisateur vs Robot IA vs Hybride vs Système)
	ReportOriginUser   = 0 // Signalement effectué manuellement par un utilisateur
	ReportOriginAutoAI = 1 // Signalement généré automatiquement par le serveur (Scoring IA + TrustScore)
	ReportOriginHybrid = 2 // Signalement initié par l'IA et confirmé par un ou plusieurs utilisateurs
	ReportOriginSystem = 3 // Super-signalement généré automatiquement par le système (ex: Quota de 3 blâmes atteint)

	ReportOriginLabelUser   = "user"
	ReportOriginLabelAutoAI = "auto_ai"
	ReportOriginLabelHybrid = "hybrid"
	ReportOriginLabelSystem = "system"

	ReportStatePending           = 0  // En attente de traitement dans la file active
	ReportStateInProgress        = 1  // En cours de traitement par un modérateur (verrou exclusif 48h)
	ReportStateWaitingForInfo    = 2  // En attente de complément d'information du plaignant (gel 7j)
	ReportStatePendingPeerReview = 3  // Proposition de blâme en attente de validation croisée (Four-Eyes)
	ReportStateEscalated         = 4  // Escaladé au niveau Administrateur (Grade 4)
	ReportStatePendingFinalVote  = 5  // Contradiction entre modérateurs soumise à un Super-Modérateur (T_mod > 0.90)
	ReportStateClosed            = -1 // Fermé / Traité et archivé en base froide PostgreSQL
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
