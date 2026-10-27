package variables

import "time"

// ============================================================================
// ÉVÉNEMENTS DE RÉPUTATION & PALIERS (IMMUABLES)
// ============================================================================

const (
	TrustEventQuarantineApproved = "QUARANTINE_APPROVED" // +0.05 (Validation manuelle d'un post en quarantaine — Cold Start)
	TrustEventMonthlyClean       = "MONTHLY_CLEAN"       // +0.05 (Ancienneté mensuelle sans infraction)
	TrustEventJustifiedReport    = "JUSTIFIED_REPORT"    // +0.03 (Signalement justifié confirmé par un modérateur)
	TrustEventAbusiveReport      = "ABUSIVE_REPORT"      // -0.15 (Signalement abusif / faux signalement)
	TrustEventMinorInfraction    = "MINOR_INFRACTION"    // -0.25 (Infraction mineure confirmée + Soft-Delete post)
	TrustEventMajorInfraction    = "MAJOR_INFRACTION"    // -1.00 (Mise à 0.00 + Révocation Sessions + Ban Stégano)

	// Dictionnaire canonique des déclencheurs ELO Asymétrique (§C.13)
	TrustTriggerVerifiedIdentity  = "verified_identity"    // +0.10 (Certification officielle d'identité)
	TrustTriggerSuccessfulAppeal  = "successful_appeal"    // +0.08 (Contestation gagnée suite à erreur de modération)
	TrustTriggerMonthlyClean      = "monthly_clean"        // +0.05 (Mois calendaire sans aucune infraction)
	TrustTriggerLikedByAdmin      = "liked_by_admin"       // +0.04 (Contenu aimé par un Modérateur ou Administrateur)
	TrustTriggerJustifiedReport   = "justified_report"     // +0.03 (Signalement citoyen menant à une sanction)
	TrustTriggerModBatchLike      = "mod_batch_like"       // +0.02 (Validation groupée de contenus qualitatifs)
	TrustTriggerSpamDetectedAuto  = "spam_detected_auto"   // -0.05 (Blocage automatique anti-flood / spam)
	TrustTriggerMutedInPublicComm = "muted_in_public_comm" // -0.10 (Exclusion / mute dans un salon communautaire public)
	TrustTriggerAbusiveReport     = "abusive_report"       // -0.15 (Signalement calomnieux ou abusif)
	TrustTriggerMinorInfraction   = "minor_infraction"     // -0.25 (Suppression d'un post hors-charte + Soft-Delete)
	TrustTriggerMajorInfraction   = "major_infraction"     // -1.00 (Bannissement définitif + Purge sessions + Ban stégano)

	TrustTierBanned     = 0 // 0.00
	TrustTierQuarantine = 1 // ]0.00, 0.39]
	TrustTierStandard   = 2 // [0.40, 0.79]
	TrustTierVIP        = 3 // [0.80, 1.00]

	TrustScoreHardBan = 0.00

	TrustScoreMin = 0.00
	TrustScoreMax = 1.00
)

// ============================================================================
// PARAMÈTRES DYNAMIQUES DU TRUST SCORE & MODÉRATION (MODIFIABLES À CHAUD)
// ============================================================================

var (
	// Initialisation, Cold Start et Seuils (Thresholds)
	TrustScoreInitial            = 0.20 // Score attribué à la création d'un nouveau compte standard
	TrustScoreInitialVIP         = 0.85 // Score initial attribué aux Administrateurs, Modérateurs et Partenaires
	TrustThresholdStandard       = 0.40 // Seuil d'accès à la publication publique instantanée
	TrustThresholdVIP            = 0.80 // Seuil d'accès à la zone de confiance / VIP
	TrustOnboardingPostsRequired = 4    // Nombre de publications validées en quarantaine requis pour sortir du Cold Start
	TrustOnboardingStepBonus     = 0.05 // Gain linéaire net par publication validée durant le Cold Start (4 * 0.05 = +0.20)

	// Coefficients d'inertie ELO Asymétrique (§C.13)
	TrustELOMinKBonus = 0.10 // Plancher minimal du facteur d'amortissement des bonus K_bonus
	TrustELOMinKMalus = 0.50 // Plancher minimal du facteur d'accélération des malus K_malus
	TrustELOMalusBase = 1.50 // Constante de base du multiplicateur de sévérité K_malus = max(0.50, 1.50 - T(u))

	// Valeurs de base Delta_base du dictionnaire des déclencheurs (§C.13)
	TrustBonusQuarantineApproved = 0.05 // Validation manuelle d'une publication en quarantaine (Cold Start)
	TrustBonusVerifiedIdentity   = 0.10 // Certification d'identité officielle
	TrustBonusSuccessfulAppeal   = 0.08 // Contestation de modération gagnée
	TrustBonusMonthlyClean       = 0.05 // Mois d'ancienneté sans aucune infraction
	TrustBonusLikedByAdmin       = 0.04 // Contenu aimé par un Modérateur ou Administrateur
	TrustBonusJustifiedReport    = 0.03 // Signalement justifié confirmé par un modérateur
	TrustBonusModBatchLike       = 0.02 // Like de masse par les modérateurs (BulkWrite)
	TrustMalusSpamDetectedAuto   = 0.05 // Détection automatique de spam / flood
	TrustMalusMutedInPublicComm  = 0.10 // Mute dans une communauté publique
	TrustMalusAbusiveReport      = 0.15 // Signalement abusif (Troll / Calomnie)
	TrustMalusMinorInfraction    = 0.25 // Infraction mineure confirmée + Soft-Delete
	TrustMalusMajorInfraction    = 1.00 // Infraction majeure (Hard Ban immédiat)

	// Intervalles des Workers de Réputation et de Modération
	TrustScoreFlushInterval      = 2 * time.Second
	TrustScoreMonthlyCronTick    = 24 * time.Hour
	ModerationAgingCronInterval  = 5 * time.Minute
	ModerationExpirationCronTick = 1 * time.Minute

	// Moteur Mathématique de Scoring IA & File de Modération Unifiée (§C.2–C.4)
	ModerationWeightIA           = 2.0  // W_ia : Poids du score IA NSFWJS (Max pour Post, 1:1 pour Message)
	ModerationWeightSignalPost   = 1.5  // W_sig (Post) : Poids historique des signalements sur une publication publique
	ModerationWeightSignalMsg    = 3.0  // W_sig (Message) : Poids agressif des signalements en messagerie privée
	ReportExpSteepness           = 4.0  // beta : Pente exponentielle récompensant les signaleurs à haut TrustScore
	ReportLogDampingFactor       = 0.50 // gamma_log : Amortissement logarithmique anti-brigading
	AIRiskDeficitFactor          = 0.50 // alpha_def : Amplification du risque IA selon (1 - T_auteur)
	AuthorTrustShieldFactor      = 1.50 // kappa_bouclier : Protection des auteurs à très haut TrustScore
	ModerationEcoLogFactor       = 0.10 // Coefficient logarithmique du bonus économique B_éco = 0.10 * ln(1 + V_éco)
	ModerationYouthBonusMax      = 5.0  // K_q : Poids initial maximal du bonus de jeunesse de compte
	ModerationYouthDecayLambda   = 0.05 // lambda_j : Taux de décroissance exponentielle journalière du bonus de jeunesse
	ModerationAgingHalfLifeHours = 12.0 // Constante horaire du multiplicateur quadratique M_temps = 1.0 + (dt / 12.0)^2

	// Multiplicateurs de Cible W_cible (§C.4.1)
	ModerationWeightTargetPost    = 1.0 // Publication publique
	ModerationWeightTargetComment = 0.8 // Commentaire
	ModerationWeightTargetProfile = 1.5 // Profil utilisateur
	ModerationWeightTargetMessage = 3.0 // Message privé (DM)
	ModerationWeightTargetConv    = 1.0 // Conversation / Salon

	// Multiplicateurs d'Origine W_origine (§C.4.2)
	ModerationWeightOriginUser   = 1.0 // Signalement humain uniquement
	ModerationWeightOriginAutoAI = 1.2 // Signalement IA automatique uniquement
	ModerationWeightOriginHybrid = 2.0 // Convergence IA + Humain (Hybride)
	ModerationWeightOriginSystem = 3.0 // Super-signalement Système (Quota de 3 blâmes atteint)

	// Gravité par Catégorie G_cat (§C.4.3 — les catégories 6 et 9 valent +Inf par invariant)
	ReportGravitySpam             = 1.0 // Catégorie 1 : Spam
	ReportGravityHarassmentMoral  = 2.0 // Catégorie 2 : Harcèlement moral / Cyberintimidation
	ReportGravityHarassmentSexual = 2.5 // Catégorie 3 : Harcèlement sexuel
	ReportGravityHateSpeech       = 3.0 // Catégorie 4 : Incitation à la haine / Violence
	ReportGravityIdentityTheft    = 2.0 // Catégorie 5 : Usurpation d'identité
	ReportGravityNonConsensual    = 4.0 // Catégorie 7 : Partage de contenu intime non consenti
	ReportGravityIllegalContent   = 3.5 // Catégorie 8 : Contenu illégal (Drogue, Armes, etc.)
	ReportGravityAIPornography    = 1.0 // Catégorie 10 : Détection IA NSFWJS
	ReportGravityOther            = 1.0 // Catégorie 99 : Autre

	// Seuils R_auto de détection automatique (§C.5–C.6)
	AIAutoReportThreshold        = 0.65 // Seuil R_auto déclenchant un signalement automatique Robot sur un Post
	AIAutoReportMessageThreshold = 0.70 // Seuil R_auto déclenchant un signalement automatique Robot sur un Message image
	AIAutoQuarantineThreshold    = 0.80 // Seuil R_auto plaçant immédiatement un Post en quarantaine préventive

	// Gouvernance, Machine à États, Verrous & Audit des Modérateurs (§C.9–C.11)
	ModerationLockTTL                = 48 * time.Hour     // Durée du verrou exclusif InProgress avant retour automatique en Pending
	ModerationWaitingInfoTTL         = 7 * 24 * time.Hour // Durée maximale d'attente d'information du plaignant avant dégel
	ModerationAdminDelegationEnabled = false              // Interrupteur de délégation administrative vers les Super-Modérateurs
	SuperModeratorTrustThreshold     = 0.90               // Seuil T_mod requis pour agir comme Super-Modérateur (arbitrage contradictoire)
	BlameQuotaForAudit               = 3                  // Nombre de blâmes déclenchant le Super-Report d'audit administrateur
	ModTrustInitial                  = 0.85               // Score de fiabilité initial T_mod attribué à un modérateur
	ModTrustValidationBonus          = 0.02               // Bonus T_mod lorsque l'administrateur confirme un audit de blâmes
	ModTrustFalseBlamePenalty        = 0.25               // Pénalité sévère T_mod en cas de blâme abusif révoqué par un administrateur
	ModTrustRevocationThreshold      = 0.50               // Seuil sous lequel les privilèges de modérateur sont révoqués automatiquement
)
