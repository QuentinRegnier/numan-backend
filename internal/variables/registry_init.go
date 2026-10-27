package variables

import (
	"errors"
	"strings"
	"time"
)

func init() {
	positiveInt := func(v int) error {
		if v <= 0 {
			return errors.New("la valeur doit être strictement positive (> 0)")
		}
		return nil
	}
	nonNegativeInt := func(v int) error {
		if v < 0 {
			return errors.New("la valeur doit être positive ou nulle (>= 0)")
		}
		return nil
	}
	negativeInt := func(v int) error {
		if v >= 0 {
			return errors.New("la valeur de rétention de rang ZSET doit être strictement négative (< 0)")
		}
		return nil
	}
	positiveInt64 := func(v int64) error {
		if v <= 0 {
			return errors.New("la valeur doit être strictement positive (> 0)")
		}
		return nil
	}
	positiveFloat := func(v float64) error {
		if v <= 0 {
			return errors.New("la valeur doit être strictement positive (> 0)")
		}
		return nil
	}
	nonNegativeFloat := func(v float64) error {
		if v < 0 {
			return errors.New("la valeur doit être positive ou nulle (>= 0)")
		}
		return nil
	}
	unitRatioFloat := func(v float64) error {
		if v < 0 || v > 1 {
			return errors.New("la valeur doit être comprise dans l'intervalle [0.0, 1.0]")
		}
		return nil
	}
	positiveDuration := func(d time.Duration) error {
		if d <= 0 {
			return errors.New("la durée doit être strictement positive (> 0)")
		}
		return nil
	}
	nonEmptyString := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New("la chaîne ne peut pas être vide")
		}
		return nil
	}

	// 1. batch_constants.go
	registerBool("MongoBulkWriteOrdered", "batch", &MongoBulkWriteOrdered)

	// 2. cleaner_constants.go
	registerDuration("LikeCleanupCronInterval", "cleaner", &LikeCleanupCronInterval, positiveDuration, nil)
	registerDuration("MediaCleanupCronInterval", "cleaner", &MediaCleanupCronInterval, positiveDuration, nil)
	registerDuration("ReactionCleanupCronInterval", "cleaner", &ReactionCleanupCronInterval, positiveDuration, nil)
	registerDuration("SavedCleanupCronInterval", "cleaner", &SavedCleanupCronInterval, positiveDuration, nil)

	// 3. comment_constants.go
	registerInt("CommentPriorityMultiplier", "comment", &CommentPriorityMultiplier, nonNegativeInt)

	// 4. conversation_constants.go
	registerInt("MaxPinConversation", "conversation", &MaxPinConversation, positiveInt)
	registerInt("MaxActiveParticipantsForBroadcast", "conversation", &MaxActiveParticipantsForBroadcast, positiveInt)
	registerInt("MaxZsetInbox", "conversation", &MaxZsetInbox, positiveInt)
	registerInt("CommunityMinCreationGrade", "conversation", &CommunityMinCreationGrade, nonNegativeInt)
	registerInt("MaxPublicCommunitiesColl", "conversation", &MaxPublicCommunitiesColl, positiveInt)

	// 5. feed_constants.go
	registerFloat64("SocialRatio", "feed", &SocialRatio, unitRatioFloat)
	registerFloat64("TagRatio", "feed", &TagRatio, unitRatioFloat)
	registerFloat64("GlobalRatio", "feed", &GlobalRatio, unitRatioFloat)
	registerDuration("FeedReloadDelay", "feed", &FeedReloadDelay, positiveDuration, nil)
	registerInt("FeedPageSize", "feed", &FeedPageSize, positiveInt)
	registerDuration("FeedWarmupCronInterval", "feed", &FeedWarmupCronInterval, positiveDuration, nil)
	registerInt64("FeedWarmupBatchSize", "feed", &FeedWarmupBatchSize, positiveInt64)
	registerInt64("FanOutVIPThreshold", "feed", &FanOutVIPThreshold, positiveInt64)
	registerInt("FanOutRetentionLimit", "feed", &FanOutRetentionLimit, negativeInt)

	// 6. graph_constants.go
	registerFloat64("GraphDecayLambda", "graph", &GraphDecayLambda, positiveFloat)
	registerFloat64("GraphLearningAlpha", "graph", &GraphLearningAlpha, unitRatioFloat)
	registerFloat64("GraphSurvivalEps", "graph", &GraphSurvivalEps, positiveFloat)
	registerInt("GraphTagMinLength", "graph", &GraphTagMinLength, positiveInt)

	// 7. interaction_constants.go
	registerInt("InteractionBatchThreshold", "interaction", &InteractionBatchThreshold, positiveInt)
	registerDuration("InteractionFlushInterval", "interaction", &InteractionFlushInterval, positiveDuration, nil)

	// 8. media_constants.go
	registerInt("MediaMaxPixels", "media", &MediaMaxPixels, positiveInt)
	registerInt("MediaMaxWidth", "media", &MediaMaxWidth, positiveInt)
	registerInt("MediaAvifQuality", "media", &MediaAvifQuality, func(v int) error {
		if v < 1 || v > 100 {
			return errors.New("la qualité AVIF doit être comprise entre 1 et 100")
		}
		return nil
	})
	registerInt("MediaAvifSpeed", "media", &MediaAvifSpeed, func(v int) error {
		if v < 0 || v > 10 {
			return errors.New("la vitesse AVIF doit être comprise entre 0 et 10")
		}
		return nil
	})

	// 9. member_constants.go
	registerInt("MaxAddMemberCount", "member", &MaxAddMemberCount, positiveInt)

	// 10. message_constants.go
	registerStringSlice("AllowedWidgetTypes", "message", &AllowedWidgetTypes, func(s []string) error {
		if len(s) == 0 {
			return errors.New("la liste des types de widgets autorisés ne peut pas être vide")
		}
		for _, item := range s {
			if strings.TrimSpace(item) == "" {
				return errors.New("un type de widget ne peut pas être vide")
			}
		}
		return nil
	})

	// 11. most_cache_constants.go
	registerInt("MaxStrictElements", "most_cache", &MaxStrictElements, positiveInt)
	registerInt("MaxTagElements", "most_cache", &MaxTagElements, positiveInt)
	registerInt("MaxTrendingTagsInRAM", "most_cache", &MaxTrendingTagsInRAM, positiveInt)
	registerInt("MaxPostsPerTagInRAM", "most_cache", &MaxPostsPerTagInRAM, positiveInt)
	registerInt("TDDMaxZSET", "most_cache", &TDDMaxZSET, positiveInt)
	registerInt("TDDCandidates", "most_cache", &TDDCandidates, positiveInt)
	registerInt("TDDFeedSize", "most_cache", &TDDFeedSize, positiveInt)
	registerFloat64("TDDSigmaHours", "most_cache", &TDDSigmaHours, positiveFloat)
	registerFloat64("TDDWeightView", "most_cache", &TDDWeightView, nonNegativeFloat)
	registerFloat64("TDDPhiReported", "most_cache", &TDDPhiReported, unitRatioFloat)
	registerFloat64("TDDHashtagWindowHours", "most_cache", &TDDHashtagWindowHours, positiveFloat)

	// 12. notification_constants.go
	registerInt("MaxZsetNotification", "notification", &MaxZsetNotification, positiveInt)
	registerDuration("PushWorkerBLPopTimeout", "notification", &PushWorkerBLPopTimeout, positiveDuration, nil)
	registerString("PushWorkerDefaultTitle", "notification", &PushWorkerDefaultTitle, nonEmptyString)
	registerString("PushWorkerDefaultBody", "notification", &PushWorkerDefaultBody, nonEmptyString)
	registerString("PushWorkerDefaultSound", "notification", &PushWorkerDefaultSound, nonEmptyString)
	registerString("PushWorkerDefaultChannel", "notification", &PushWorkerDefaultChannel, nonEmptyString)

	// 13. object_cache_constant.go
	registerDuration("StandardTTL", "object_cache", &StandardTTL, positiveDuration, func(d time.Duration) {
		if standardTTLHook != nil {
			standardTTLHook(d)
		}
	})
	registerInt("MaxZsetPostComment", "object_cache", &MaxZsetPostComment, positiveInt)

	// 14. post_constants.go
	registerInt("MaxHashtagsPerPost", "post", &MaxHashtagsPerPost, positiveInt)
	registerInt("MaxIdentifiersPerPost", "post", &MaxIdentifiersPerPost, positiveInt)
	registerInt("MaxMediaPerPost", "post", &MaxMediaPerPost, positiveInt)

	// 15. recommendation_constants.go
	registerFloat64("TDDWeightLike", "recommendation", &TDDWeightLike, nonNegativeFloat)
	registerFloat64("TDDWeightComment", "recommendation", &TDDWeightComment, nonNegativeFloat)
	registerFloat64("TDDWeightMedia", "recommendation", &TDDWeightMedia, nonNegativeFloat)
	registerFloat64("TDDAlpha", "recommendation", &TDDAlpha, positiveFloat)
	registerFloat64("TDDBeta", "recommendation", &TDDBeta, positiveFloat)
	registerFloat64("TDDTheta", "recommendation", &TDDTheta, positiveFloat)
	registerFloat64("TDDLambdaDecay", "recommendation", &TDDLambdaDecay, positiveFloat)
	registerFloat64("TDDTGrace", "recommendation", &TDDTGrace, nonNegativeFloat)
	registerFloat64("TDDPhiMedia", "recommendation", &TDDPhiMedia, nonNegativeFloat)
	registerFloat64("TDDPhiGrade", "recommendation", &TDDPhiGrade, nonNegativeFloat)
	registerFloat64("TDDGradeMax", "recommendation", &TDDGradeMax, positiveFloat)
	registerFloat64("TDDGammaAuth", "recommendation", &TDDGammaAuth, nonNegativeFloat)
	registerFloat64("TDDRho", "recommendation", &TDDRho, unitRatioFloat)
	registerFloat64("TDDEta", "recommendation", &TDDEta, nonNegativeFloat)
	registerFloat64("TDDEtaP", "recommendation", &TDDEtaP, nonNegativeFloat)
	registerFloat64("TDDLambdaMMR", "recommendation", &TDDLambdaMMR, unitRatioFloat)
	registerFloat64("TDDDeltaInvalid", "recommendation", &TDDDeltaInvalid, unitRatioFloat)
	registerFloat64("TDDLSHConfidenceThreshold", "recommendation", &TDDLSHConfidenceThreshold, unitRatioFloat)
	registerDuration("TrendCronInterval", "recommendation", &TrendCronInterval, positiveDuration, nil)
	registerInt64("TrendTopPostsLimit", "recommendation", &TrendTopPostsLimit, positiveInt64)
	registerInt("TrendTagsRetention", "recommendation", &TrendTagsRetention, negativeInt)
	registerDuration("HashtagCanonCronInterval", "recommendation", &HashtagCanonCronInterval, positiveDuration, nil)
	registerInt("HashtagCanonMinLength", "recommendation", &HashtagCanonMinLength, positiveInt)
	registerFloat64("HashtagCanonMaxDistance", "recommendation", &HashtagCanonMaxDistance, unitRatioFloat)

	// 16. reports_constants.go
	registerFloat64("ReportScoreFollowerMultiplier", "reports", &ReportScoreFollowerMultiplier, nonNegativeFloat)
	registerFloat64("ReportScoreCertifiedBonus", "reports", &ReportScoreCertifiedBonus, nonNegativeFloat)
	registerFloat64("ReportScorePartnerBonus", "reports", &ReportScorePartnerBonus, nonNegativeFloat)
	registerFloat64("ReportScoreStaffBonus", "reports", &ReportScoreStaffBonus, nonNegativeFloat)
	registerFloat64("ReportScoreViewMultiplier", "reports", &ReportScoreViewMultiplier, nonNegativeFloat)
	registerFloat64("ReportScoreLikeMultiplier", "reports", &ReportScoreLikeMultiplier, nonNegativeFloat)
	registerFloat64("ReportScoreCommentMultiplier", "reports", &ReportScoreCommentMultiplier, nonNegativeFloat)
	registerFloat64("ReportScoreDwellMultiplier", "reports", &ReportScoreDwellMultiplier, nonNegativeFloat)
	registerFloat64("ReportScoreCommentLikeMultiplier", "reports", &ReportScoreCommentLikeMultiplier, nonNegativeFloat)
	registerInt("MaxReportTargets", "reports", &MaxReportTargets, positiveInt)

	// 17. search_constants.go
	registerInt64("DefaultAutocompleteLimit", "search", &DefaultAutocompleteLimit, positiveInt64)
	registerInt64("DefaultGlobalSearchLimit", "search", &DefaultGlobalSearchLimit, positiveInt64)

	// 18. standart_constants.go
	registerInt("FetchBatchLimit", "standard", &FetchBatchLimit, positiveInt)
	registerInt("ToleranceTimeSeconds", "standard", &ToleranceTimeSeconds, positiveInt)
	registerInt("JWTExpirationSeconds", "standard", &JWTExpirationSeconds, positiveInt)
	registerInt("MasterTokenExpirationSeconds", "standard", &MasterTokenExpirationSeconds, positiveInt)

	// 19. telemetry_constants.go
	registerInt("MaxGhostScrollTime", "telemetry", &MaxGhostScrollTime, positiveInt)
	registerInt("MinCognitiveReadTime", "telemetry", &MinCognitiveReadTime, positiveInt)

	// 20. two_factor_constants.go
	registerDuration("TwoFactorPreAuthTTL", "security", &TwoFactorPreAuthTTL, positiveDuration, nil)
	registerInt("TwoFactorMaxAttempts", "security", &TwoFactorMaxAttempts, positiveInt)
	registerDuration("WebAuthnTimeoutMs", "security", &WebAuthnTimeoutMs, positiveDuration, nil)

	// 21. trust_score_constants.go
	registerFloat64("TrustScoreInitial", "trust_score", &TrustScoreInitial, unitRatioFloat)
	registerFloat64("TrustScoreInitialVIP", "trust_score", &TrustScoreInitialVIP, unitRatioFloat)
	registerFloat64("TrustThresholdStandard", "trust_score", &TrustThresholdStandard, unitRatioFloat)
	registerFloat64("TrustThresholdVIP", "trust_score", &TrustThresholdVIP, unitRatioFloat)
	registerInt("TrustOnboardingPostsRequired", "trust_score", &TrustOnboardingPostsRequired, positiveInt)
	registerFloat64("TrustOnboardingStepBonus", "trust_score", &TrustOnboardingStepBonus, unitRatioFloat)
	registerFloat64("TrustELOMinKBonus", "trust_score", &TrustELOMinKBonus, unitRatioFloat)
	registerFloat64("TrustELOMinKMalus", "trust_score", &TrustELOMinKMalus, positiveFloat)
	registerFloat64("TrustELOMalusBase", "trust_score", &TrustELOMalusBase, positiveFloat)
	registerFloat64("TrustBonusQuarantineApproved", "trust_score", &TrustBonusQuarantineApproved, unitRatioFloat)
	registerFloat64("TrustBonusVerifiedIdentity", "trust_score", &TrustBonusVerifiedIdentity, unitRatioFloat)
	registerFloat64("TrustBonusSuccessfulAppeal", "trust_score", &TrustBonusSuccessfulAppeal, unitRatioFloat)
	registerFloat64("TrustBonusMonthlyClean", "trust_score", &TrustBonusMonthlyClean, unitRatioFloat)
	registerFloat64("TrustBonusLikedByAdmin", "trust_score", &TrustBonusLikedByAdmin, unitRatioFloat)
	registerFloat64("TrustBonusJustifiedReport", "trust_score", &TrustBonusJustifiedReport, unitRatioFloat)
	registerFloat64("TrustBonusModBatchLike", "trust_score", &TrustBonusModBatchLike, unitRatioFloat)
	registerFloat64("TrustMalusSpamDetectedAuto", "trust_score", &TrustMalusSpamDetectedAuto, unitRatioFloat)
	registerFloat64("TrustMalusMutedInPublicComm", "trust_score", &TrustMalusMutedInPublicComm, unitRatioFloat)
	registerFloat64("TrustMalusAbusiveReport", "trust_score", &TrustMalusAbusiveReport, unitRatioFloat)
	registerFloat64("TrustMalusMinorInfraction", "trust_score", &TrustMalusMinorInfraction, unitRatioFloat)
	registerFloat64("TrustMalusMajorInfraction", "trust_score", &TrustMalusMajorInfraction, unitRatioFloat)
	registerDuration("TrustScoreFlushInterval", "trust_score", &TrustScoreFlushInterval, positiveDuration, nil)
	registerDuration("TrustScoreMonthlyCronTick", "trust_score", &TrustScoreMonthlyCronTick, positiveDuration, nil)
	registerDuration("ModerationAgingCronInterval", "trust_score", &ModerationAgingCronInterval, positiveDuration, nil)
	registerDuration("ModerationExpirationCronTick", "trust_score", &ModerationExpirationCronTick, positiveDuration, nil)
	registerFloat64("ModerationWeightIA", "trust_score", &ModerationWeightIA, positiveFloat)
	registerFloat64("ModerationWeightSignalPost", "trust_score", &ModerationWeightSignalPost, positiveFloat)
	registerFloat64("ModerationWeightSignalMsg", "trust_score", &ModerationWeightSignalMsg, positiveFloat)
	registerFloat64("ReportExpSteepness", "trust_score", &ReportExpSteepness, positiveFloat)
	registerFloat64("ReportLogDampingFactor", "trust_score", &ReportLogDampingFactor, nonNegativeFloat)
	registerFloat64("AIRiskDeficitFactor", "trust_score", &AIRiskDeficitFactor, nonNegativeFloat)
	registerFloat64("AuthorTrustShieldFactor", "trust_score", &AuthorTrustShieldFactor, nonNegativeFloat)
	registerFloat64("ModerationEcoLogFactor", "trust_score", &ModerationEcoLogFactor, nonNegativeFloat)
	registerFloat64("ModerationYouthBonusMax", "trust_score", &ModerationYouthBonusMax, nonNegativeFloat)
	registerFloat64("ModerationYouthDecayLambda", "trust_score", &ModerationYouthDecayLambda, positiveFloat)
	registerFloat64("ModerationAgingHalfLifeHours", "trust_score", &ModerationAgingHalfLifeHours, positiveFloat)
	registerFloat64("ModerationWeightTargetPost", "trust_score", &ModerationWeightTargetPost, positiveFloat)
	registerFloat64("ModerationWeightTargetComment", "trust_score", &ModerationWeightTargetComment, positiveFloat)
	registerFloat64("ModerationWeightTargetProfile", "trust_score", &ModerationWeightTargetProfile, positiveFloat)
	registerFloat64("ModerationWeightTargetMessage", "trust_score", &ModerationWeightTargetMessage, positiveFloat)
	registerFloat64("ModerationWeightTargetConv", "trust_score", &ModerationWeightTargetConv, positiveFloat)
	registerFloat64("ModerationWeightOriginUser", "trust_score", &ModerationWeightOriginUser, positiveFloat)
	registerFloat64("ModerationWeightOriginAutoAI", "trust_score", &ModerationWeightOriginAutoAI, positiveFloat)
	registerFloat64("ModerationWeightOriginHybrid", "trust_score", &ModerationWeightOriginHybrid, positiveFloat)
	registerFloat64("ModerationWeightOriginSystem", "trust_score", &ModerationWeightOriginSystem, positiveFloat)
	registerFloat64("ReportGravitySpam", "trust_score", &ReportGravitySpam, positiveFloat)
	registerFloat64("ReportGravityHarassmentMoral", "trust_score", &ReportGravityHarassmentMoral, positiveFloat)
	registerFloat64("ReportGravityHarassmentSexual", "trust_score", &ReportGravityHarassmentSexual, positiveFloat)
	registerFloat64("ReportGravityHateSpeech", "trust_score", &ReportGravityHateSpeech, positiveFloat)
	registerFloat64("ReportGravityIdentityTheft", "trust_score", &ReportGravityIdentityTheft, positiveFloat)
	registerFloat64("ReportGravityNonConsensual", "trust_score", &ReportGravityNonConsensual, positiveFloat)
	registerFloat64("ReportGravityIllegalContent", "trust_score", &ReportGravityIllegalContent, positiveFloat)
	registerFloat64("ReportGravityAIPornography", "trust_score", &ReportGravityAIPornography, positiveFloat)
	registerFloat64("ReportGravityOther", "trust_score", &ReportGravityOther, positiveFloat)
	registerFloat64("AIAutoReportThreshold", "trust_score", &AIAutoReportThreshold, unitRatioFloat)
	registerFloat64("AIAutoReportMessageThreshold", "trust_score", &AIAutoReportMessageThreshold, unitRatioFloat)
	registerFloat64("AIAutoQuarantineThreshold", "trust_score", &AIAutoQuarantineThreshold, unitRatioFloat)
	registerBool("ModerationAdminDelegationEnabled", "trust_score", &ModerationAdminDelegationEnabled)
	registerFloat64("SuperModeratorTrustThreshold", "trust_score", &SuperModeratorTrustThreshold, unitRatioFloat)
	registerInt("BlameQuotaForAudit", "trust_score", &BlameQuotaForAudit, positiveInt)
	registerFloat64("ModTrustInitial", "trust_score", &ModTrustInitial, unitRatioFloat)
	registerFloat64("ModTrustValidationBonus", "trust_score", &ModTrustValidationBonus, unitRatioFloat)
	registerFloat64("ModTrustFalseBlamePenalty", "trust_score", &ModTrustFalseBlamePenalty, unitRatioFloat)
	registerFloat64("ModTrustRevocationThreshold", "trust_score", &ModTrustRevocationThreshold, unitRatioFloat)
}
