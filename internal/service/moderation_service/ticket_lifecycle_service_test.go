package moderation_service

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/logger"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTicketLifecycleService_CompleteWorkflow(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		return test.NewMockRows([]string{"archived"}, [][]driver.Value{{true}}), nil
	}, nil)

	baseTime := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	restoreClock := SetLifecycleClock(func() time.Time { return baseTime })
	defer restoreClock()

	// Initialisation d'un ticket en attente
	ticketID := int64(8001)
	authorID := int64(7001)
	postID := int64(9001)
	modA := int64(3001)
	modB := int64(3002)
	reporterID := int64(6001)

	// Post initial public
	require.NoError(t, object_cache_service.SetPostInObjectCache(ctx, post_models.PostPayload{
		ID:         postID,
		UserID:     authorID,
		Content:    "Post sous modération",
		Visibility: variables.PostVisibilityAll,
	}))

	ticket := &moderation_models.ModerationTicketRecord{
		ReportID:      ticketID,
		TargetType:    variables.ReportTargetPost,
		TargetID:      postID,
		TargetOwnerID: authorID,
		Category:      variables.ReportCatHarassmentMoral,
		ReportOrigin:  variables.ReportOriginUser,
		State:         variables.ReportStatePending,
		Reason:        "Harcèlement ciblé",
		PriorityScore: 25.0,
		ReporterIDs:   []int64{reporterID},
		CreatedAt:     baseTime.UnixMilli(),
		UpdatedAt:     baseTime.UnixMilli(),
	}
	require.NoError(t, cache_service.SaveModerationTicket(ctx, ticket))
	require.NoError(t, cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueueActive, ticketID, 25.0))

	// 1. ClaimTicket : Mod A réclame le ticket
	view, errClaim := ClaimTicketService(ctx, modA, ticketID)
	require.NoError(t, errClaim)
	assert.Equal(t, variables.ReportStateInProgress, view.State)
	assert.Equal(t, modA, view.ClaimedByModID)

	// Concurrence : Mod B ne peut pas réclamer le même ticket
	_, errClaimConflict := ClaimTicketService(ctx, modB, ticketID)
	require.Error(t, errClaimConflict)
	var conflictErr *numan_error.AppError
	require.ErrorAs(t, errClaimConflict, &conflictErr)
	assert.Equal(t, numan_error.CodeConflict, conflictErr.Code)

	// 2. RequestTicketInfo & ReplyTicketInfo
	viewWaiting, errWait := RequestTicketInfoService(ctx, modA, variables.UserGradeModerator, moderation_models.RequestTicketInfoInput{
		ReportID: ticketID,
		Question: "Pouvez-vous fournir des captures d'écran ?",
	})
	require.NoError(t, errWait)
	assert.Equal(t, variables.ReportStateWaitingForInfo, viewWaiting.State)

	// Seul le plaignant peut répondre
	_, errFakeReply := ReplyTicketInfoService(ctx, 9999, variables.UserGradeUser, moderation_models.ReplyTicketInfoInput{
		ReportID: ticketID,
		Reply:    "Tiers non autorisé",
	})
	require.Error(t, errFakeReply)

	viewResumed, errReply := ReplyTicketInfoService(ctx, reporterID, variables.UserGradeUser, moderation_models.ReplyTicketInfoInput{
		ReportID: ticketID,
		Reply:    "Voici le complément demandé.",
	})
	require.NoError(t, errReply)
	assert.Equal(t, variables.ReportStateInProgress, viewResumed.State)
	assert.Equal(t, modA, viewResumed.ClaimedByModID)

	// 3. ProposeBlame : Mod A propose un blâme sous pseudonyme HMAC
	viewPeer, errPropose := ProposeBlameService(ctx, modA, variables.UserGradeModerator, moderation_models.ProposeBlameInput{
		ReportID:   ticketID,
		Motivation: "Infraction avérée aux règles de vie en communauté",
	})
	require.NoError(t, errPropose)
	assert.Equal(t, variables.ReportStatePendingPeerReview, viewPeer.State)
	assert.NotEmpty(t, viewPeer.ProposerPseudonym)
	assert.Equal(t, int64(0), viewPeer.ClaimedByModID, "l'identité de Mod A doit être masquée dans la vue publique")

	// 4. ReviewBlameProposal : Mod A tente de s'auto-valider (Interdiction absolue sans fuite d'identité)
	_, errSelfReview := ReviewBlameProposalService(ctx, modA, variables.UserGradeModerator, moderation_models.ReviewBlameInput{
		ReportID:  ticketID,
		Decision:  "validate",
		Rationale: "Tentative d'auto-validation frauduleuse",
	})
	require.Error(t, errSelfReview)
	var forbiddenErr *numan_error.AppError
	require.ErrorAs(t, errSelfReview, &forbiddenErr)
	assert.Equal(t, numan_error.CodeForbidden, forbiddenErr.Code)

	// Mod B valide le blâme -> Consensus Four-Eyes
	viewClosed, errReview := ReviewBlameProposalService(ctx, modB, variables.UserGradeModerator, moderation_models.ReviewBlameInput{
		ReportID:  ticketID,
		Decision:  "validate",
		Rationale: "Blâme tout à fait justifié après examen des preuves",
	})
	require.NoError(t, errReview)
	assert.Equal(t, variables.ReportStateClosed, viewClosed.State)
	assert.Equal(t, "blame_validated", viewClosed.ResolutionOutcome)

	// Vérifie que le post est soft-deleted (Visibility = -1)
	softPost, _ := object_cache_service.GetPostFromObjectCache(ctx, postID)
	assert.Equal(t, variables.PostVisibilitySoftDeleted, softPost.Visibility)

	// Vérifie le compteur de blâmes de la cible
	authorStats, errStats := cache_service.GetOrInitModeratorStats(ctx, authorID, 0)
	require.NoError(t, errStats)
	assert.Equal(t, 1, authorStats.BlameCount)
	require.Len(t, authorStats.BlameHistory, 1)
	assert.Equal(t, modA, authorStats.BlameHistory[0].ProposerModID)
	assert.Equal(t, modB, authorStats.BlameHistory[0].ValidatorModID)
}

func TestTicketLifecycleService_ContradictionRoutingAndSuperModerator(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		return test.NewMockRows([]string{"archived"}, [][]driver.Value{{true}}), nil
	}, nil)

	ticketID := int64(8002)
	modA := int64(3001)
	modB := int64(3002)
	modC := int64(3003) // Super-modérateur (T_mod = 0.95)

	tRecord := &moderation_models.ModerationTicketRecord{
		ReportID:          ticketID,
		TargetType:        variables.ReportTargetComment,
		TargetID:          9002,
		TargetOwnerID:     7002,
		State:             variables.ReportStatePendingPeerReview,
		ProposerModID:     modA,
		ProposerPseudonym: "mod_anon_test123",
		BlameMotivation:   "Commentaire offensant",
		PriorityScore:     30.0,
	}
	require.NoError(t, cache_service.SaveModerationTicket(ctx, tRecord))
	require.NoError(t, cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueuePeerReview, ticketID, 30.0))

	// A. Contradiction avec Délégation OFF -> Escalade en file Admin
	variables.ModerationAdminDelegationEnabled = false
	viewAdmin, errOpposeAdmin := ReviewBlameProposalService(ctx, modB, variables.UserGradeModerator, moderation_models.ReviewBlameInput{
		ReportID:  ticketID,
		Decision:  "oppose",
		Rationale: "Désaccord : il s'agit d'une simple critique acceptable",
	})
	require.NoError(t, errOpposeAdmin)
	assert.Equal(t, variables.ReportStateEscalated, viewAdmin.State)
	assert.Equal(t, "blame_contradiction", viewAdmin.EscalationType)

	// B. Contradiction avec Délégation ON -> Redirection en file FinalVote
	variables.ModerationAdminDelegationEnabled = true
	tRecord.State = variables.ReportStatePendingPeerReview
	require.NoError(t, cache_service.SaveModerationTicket(ctx, tRecord))
	require.NoError(t, cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueuePeerReview, ticketID, 30.0))

	viewSuperMod, errOpposeSuper := ReviewBlameProposalService(ctx, modB, variables.UserGradeModerator, moderation_models.ReviewBlameInput{
		ReportID:  ticketID,
		Decision:  "oppose",
		Rationale: "Désaccord sur le caractère injurieux",
	})
	require.NoError(t, errOpposeSuper)
	assert.Equal(t, variables.ReportStatePendingFinalVote, viewSuperMod.State)

	// Résolution par le Super-Modérateur Mod C (T_mod = 0.95)
	modCStats := &moderation_models.ModeratorStatsRecord{
		UserID:              modC,
		Grade:               variables.UserGradeModerator,
		ModeratorTrustScore: 0.95,
	}
	require.NoError(t, cache_service.SaveModeratorStats(ctx, modCStats))

	viewResolved, errResolve := ResolveFinalVoteService(ctx, modC, variables.UserGradeModerator, moderation_models.FinalVoteBlameInput{
		ReportID:  ticketID,
		Decision:  "reject_blame",
		Rationale: "Arbitrage final : le contenu n'enfreint pas la charte",
	})
	require.NoError(t, errResolve)
	assert.Equal(t, variables.ReportStateClosed, viewResolved.State)
	assert.Equal(t, "reject_blame", viewResolved.ResolutionOutcome)
}

func TestTicketLifecycleService_ThreeBlameAuditAndAdminOutcomes(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		return test.NewMockRows([]string{"archived"}, [][]driver.Value{{true}}), nil
	}, nil)

	targetID := int64(7003)
	modA := int64(3001)
	modB := int64(3002)
	adminID := int64(4001)

	// L'auteur a déjà 2 blâmes
	targetStats := &moderation_models.ModeratorStatsRecord{
		UserID:     targetID,
		BlameCount: 2,
		BlameHistory: []moderation_models.BlameRecord{
			{ReportID: 1, ProposerModID: modA, ValidatorModID: modB, Motivation: "Blame 1"},
			{ReportID: 2, ProposerModID: modA, ValidatorModID: modB, Motivation: "Blame 2"},
		},
	}
	require.NoError(t, cache_service.SaveModeratorStats(ctx, targetStats))

	// 3ème blâme appliqué -> Déclenchement automatique du Super-Report d'audit sans auto-ban
	ticket3 := &moderation_models.ModerationTicketRecord{
		ReportID:          8003,
		TargetType:        variables.ReportTargetPost,
		TargetID:          9003,
		TargetOwnerID:     targetID,
		State:             variables.ReportStatePendingPeerReview,
		ProposerModID:     modA,
		ProposerPseudonym: "mod_anon_a",
		BlameMotivation:   "Troisième récidive",
		PriorityScore:     50.0,
	}
	require.NoError(t, cache_service.SaveModerationTicket(ctx, ticket3))
	require.NoError(t, cache_service.IndexTicketInModerationQueue(ctx, variables.ModerationQueuePeerReview, ticket3.ReportID, 50.0))

	_, errThirdBlame := ReviewBlameProposalService(ctx, modB, variables.UserGradeModerator, moderation_models.ReviewBlameInput{
		ReportID:  8003,
		Decision:  "validate",
		Rationale: "Troisième blâme validé",
	})
	require.NoError(t, errThirdBlame)

	// Vérifie qu'un Super-Report d'audit est présent dans la file Admin
	adminTicketIDs, _, errList := cache_service.ListModerationQueueTicketIDs(ctx, variables.ModerationQueueAdmin, 0, 10)
	require.NoError(t, errList)
	require.NotEmpty(t, adminTicketIDs)

	superReportID := adminTicketIDs[0]
	superReport, errSuper := cache_service.GetModerationTicket(ctx, superReportID)
	require.NoError(t, errSuper)
	assert.Equal(t, variables.ReportOriginSystem, superReport.ReportOrigin)
	assert.Equal(t, "blame_quota_audit", superReport.EscalationType)
	assert.Len(t, superReport.AuditBlameRecords, 3)

	// Résolution Administrateur :
	// Test avec valeurs réelles par défaut (T_mod initial = 0.85)
	modC := int64(8003)
	// Initialise les modérateurs au score officiel par défaut (0.85)
	require.NoError(t, cache_service.SaveModeratorStats(ctx, &moderation_models.ModeratorStatsRecord{
		UserID:              modA,
		Grade:               variables.UserGradeModerator,
		ModeratorTrustScore: variables.ModTrustInitial, // 0.85
	}))
	require.NoError(t, cache_service.SaveModeratorStats(ctx, &moderation_models.ModeratorStatsRecord{
		UserID:              modB,
		Grade:               variables.UserGradeModerator,
		ModeratorTrustScore: variables.ModTrustInitial, // 0.85
	}))
	require.NoError(t, cache_service.SaveModeratorStats(ctx, &moderation_models.ModeratorStatsRecord{
		UserID:              modC,
		Grade:               variables.UserGradeModerator,
		ModeratorTrustScore: variables.ModTrustInitial, // 0.85
	}))

	// Affecte le Blâme 3 à ModA et ModC pour tester la révocation partielle
	superReport.AuditBlameRecords[2].ProposerModID = modA
	superReport.AuditBlameRecords[2].ValidatorModID = modC
	require.NoError(t, cache_service.SaveModerationTicket(ctx, superReport))

	// 1. Révocation PARTIELLE : l'admin ne révoque que le blâme 1 (sur les 3)
	revokedReportID := superReport.AuditBlameRecords[0].ReportID
	partialAuditRes, errPartialAudit := ResolveAdminAuditService(ctx, adminID, variables.UserGradeAdmin, moderation_models.ResolveAdminAuditInput{
		ReportID:              superReportID,
		Decision:              "revoke",
		Rationale:             "Le premier blâme était infondé, les deux autres restent à l'étude.",
		RevokedBlameReportIDs: []int64{revokedReportID},
	})
	require.NoError(t, errPartialAudit)
	assert.False(t, partialAuditRes.TargetBanned)
	assert.Contains(t, partialAuditRes.PenalizedModIDs, modA)
	assert.Contains(t, partialAuditRes.PenalizedModIDs, modB)
	assert.NotContains(t, partialAuditRes.PenalizedModIDs, modC, "ModC n'a pas instruit le blâme 1 et ne doit pas être pénalisé")
	assert.Empty(t, partialAuditRes.DemotedModIDs, "Avec 0.85 au départ, une seule pénalité de -0.25 (-> 0.60) ne rétrograde pas (seuil = 0.50)")

	// Vérifie que ModA est passé de 0.85 à 0.60
	modAStats1, _ := cache_service.GetOrInitModeratorStats(ctx, modA, 0)
	assert.InDelta(t, 0.60, modAStats1.ModeratorTrustScore, 1e-4)
	assert.Equal(t, variables.UserGradeModerator, modAStats1.Grade)

	// Vérifie que le compteur de blâmes de la cible est à 2 (3 - 1 révoqué)
	targetStatsPartial, _ := cache_service.GetOrInitModeratorStats(ctx, targetID, 0)
	assert.Equal(t, 2, targetStatsPartial.BlameCount)

	// Vérifie la publication de l'événement de réputation TrustTriggerSuccessfulAppeal (+0.08)
	rawAppeal, errAppeal := redis.WorkerQueue.LPop(ctx, variables.WorkerQueueTrustScore)
	require.NoError(t, errAppeal)
	assert.Contains(t, rawAppeal, variables.TrustTriggerSuccessfulAppeal)
	assert.Contains(t, rawAppeal, "5001")

	// 2. Révocation TOTALE : réouverture pour audit des blâmes restants et révocation complète
	superReport.State = variables.ReportStateEscalated
	require.NoError(t, cache_service.SaveModerationTicket(ctx, superReport))

	fullAuditRes, errFullAudit := ResolveAdminAuditService(ctx, adminID, variables.UserGradeAdmin, moderation_models.ResolveAdminAuditInput{
		ReportID:  superReportID,
		Decision:  "revoke",
		Rationale: "Après réexamen approfondi, tous les blâmes sont annulés.",
	})
	require.NoError(t, errFullAudit)
	assert.Contains(t, fullAuditRes.PenalizedModIDs, modA)
	assert.Contains(t, fullAuditRes.PenalizedModIDs, modC)

	// ModA subit une 2ème pénalité : 0.60 - 0.25 = 0.35 (< 0.50) -> Rétrogradation automatique au Grade User (0) !
	assert.Contains(t, fullAuditRes.DemotedModIDs, modA)
	modAStats2, _ := cache_service.GetOrInitModeratorStats(ctx, modA, 0)
	assert.Equal(t, variables.UserGradeUser, modAStats2.Grade)
	assert.InDelta(t, 0.35, modAStats2.ModeratorTrustScore, 1e-4)

	// Vérifie que le compteur de la cible est désormais totalement réinitialisé à 0
	restoredTargetStats, _ := cache_service.GetOrInitModeratorStats(ctx, targetID, 0)
	assert.Equal(t, 0, restoredTargetStats.BlameCount)
}

func TestTicketLifecycleService_HardBanAndDelegationSwitch(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	adminID := int64(4001)
	targetID := int64(7004)

	// Prépare un profil UserLite actif dans le cache SPEED L1
	uLite := lite_models.UserLiteRequest{
		ID:         targetID,
		Username:   "target_violator",
		TrustScore: 0.75,
		Grade:      0,
	}
	require.NoError(t, redis.UsersLite.SetObject(ctx, targetID, uLite))

	// Hard Ban exécuté par l'Administration
	banRes, errBan := ExecuteAdminHardBanService(ctx, adminID, moderation_models.AdminHardBanInput{
		TargetUserID:    targetID,
		Reason:          "Pédopornographie avérée (CSAM)",
		CGUFeedbackNote: "Infraction d'une gravité absolue. Signalement PHAROS effectué.",
	})
	require.NoError(t, errBan)
	assert.True(t, banRes.Banned)
	assert.Equal(t, variables.TrustScoreHardBan, banRes.TrustScore)

	// Assertions strictes du Hard Ban :
	// 1. T = 0.00 et compte mis à jour dans le cache SPEED L1
	bannedUserLite, errBannedLite := cache_service.GetUserLite(ctx, targetID)
	require.NoError(t, errBannedLite)
	assert.Equal(t, variables.TrustScoreHardBan, bannedUserLite.TrustScore, "Le Trust Score doit être réinitialisé à 0.00")

	// 2. Bannissement stéganographique enregistré dans le Set Redis O(1)
	isStegano, errStegano := redis.SteganoBanlist.SIsMember(ctx, "global", fmt.Sprintf("%d", targetID))
	require.NoError(t, errStegano)
	assert.True(t, isStegano, "L'identifiant stéganographique doit être inscrit dans le Set Redis")

	// Interrupteur de délégation
	switchRes, errSwitch := SetDelegationSwitchService(ctx, variables.UserGradeAdmin, true)
	require.NoError(t, errSwitch)
	assert.True(t, switchRes.Enabled)
	assert.True(t, GetDelegationSwitchService(ctx))

	// Rejet si modérateur normal tente de basculer l'interrupteur
	_, errForbiddenSwitch := SetDelegationSwitchService(ctx, variables.UserGradeModerator, false)
	require.Error(t, errForbiddenSwitch)

	// Clôture explicite d'un ticket avec feedback signaleurs
	tClose := &moderation_models.ModerationTicketRecord{
		ReportID:    8099,
		ReporterIDs: []int64{6001},
		State:       variables.ReportStateInProgress,
	}
	require.NoError(t, cache_service.SaveModerationTicket(ctx, tClose))
	_, errClose := CloseTicketService(ctx, adminID, false, "Ticket clos avec succès", 8099)
	require.NoError(t, errClose)
}

func TestTicketLifecycleService_PeerReviewAnonymity(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	test.SetupMockDB(t, func(query string, args []driver.NamedValue) (driver.Rows, error) {
		if strings.Contains(query, "moderation.func_load_quarantined_media_paginated") {
			return test.NewMockRows([]string{"id", "owner_id", "storage_path", "visibility", "created_at", "updated_at"}, nil), nil
		}
		if strings.Contains(query, "moderation.func_load_pending_reports_paginated") {
			return test.NewMockRows([]string{
				"id", "reporter_id", "report_origin", "target_type", "target_ids",
				"target_owner_id", "author_trust_score", "ia_score_max", "accumulated_report_score", "report_count",
				"category", "reason", "rationale", "state", "importance", "created_at", "updated_at",
			}, nil), nil
		}
		return test.NewMockRows([]string{}, nil), nil
	}, nil)

	modA := int64(998877)
	modB := int64(445566)
	targetID := int64(112233)
	ticketID := int64(778899)

	// Inscription de Mod B dans le cache Speed pour GetModerationQueue
	modBLite := lite_models.UserLiteRequest{
		ID:         modB,
		Grade:      variables.UserGradeModerator,
		TrustScore: 0.85,
	}
	require.NoError(t, cache_service.UpdateUserLiteInSpeedCache(ctx, modBLite))

	// Capture des logs zerolog dans un buffer pour prouver l'absence de fuite dans les traces
	var logBuf bytes.Buffer
	origLogger := logger.Log
	logger.Log = zerolog.New(&logBuf)
	defer func() {
		logger.Log = origLogger
	}()

	// Ticket claimed by Mod A
	ticket := &moderation_models.ModerationTicketRecord{
		ReportID:        ticketID,
		TargetType:      variables.ReportTargetPost,
		TargetID:        1001,
		TargetOwnerID:   targetID,
		Category:        variables.ReportCatHarassmentMoral,
		ReportOrigin:    variables.ReportOriginUser,
		State:           variables.ReportStateInProgress,
		Reason:          "Insultes graves",
		ClaimedByModID:  modA,
		LockExpiresAtMs: time.Now().Add(48 * time.Hour).UnixMilli(),
		CreatedAt:       time.Now().UnixMilli(),
		UpdatedAt:       time.Now().UnixMilli(),
	}
	require.NoError(t, cache_service.SaveModerationTicket(ctx, ticket))

	// Mod A propose un blâme
	proposeRes, errPropose := ProposeBlameService(ctx, modA, variables.UserGradeModerator, moderation_models.ProposeBlameInput{
		ReportID:   ticketID,
		Motivation: "Harcèlement caractérisé et répété envers la victime.",
	})
	require.NoError(t, errPropose)
	require.NotNil(t, proposeRes)

	// (a.1) Vérification de la réponse directe de proposition de blâme
	proposeJSON, errPJSON := json.Marshal(proposeRes)
	require.NoError(t, errPJSON)
	assert.NotContains(t, string(proposeJSON), "998877", "La réponse de proposition ne doit jamais faire fuiter l'ID réel de Mod A")
	assert.NotContains(t, string(proposeJSON), "proposer_mod_id")
	assert.NotContains(t, string(proposeJSON), "validator_mod_id")
	assert.NotContains(t, string(proposeJSON), "claimed_by_mod_id")

	// (a.2) Mod B consulte la file d'attente peer_review via GetModerationQueue
	queueOut, errQueue := GetModerationQueue(ctx, modB, moderation_models.GetModerationQueueInput{
		QueueType: variables.ModerationQueuePeerReview,
	})
	require.NoError(t, errQueue)
	require.NotEmpty(t, queueOut.QueueTickets)

	queueJSON, errQJSON := json.Marshal(queueOut)
	require.NoError(t, errQJSON)
	qStr := string(queueJSON)

	// Assertions strictes d'anonymat sur le payload de la file d'attente reçue par Mod B :
	assert.NotContains(t, qStr, "998877", "L'identifiant réel du Modérateur A (998877) ne doit jamais apparaître dans la liste des tickets")
	assert.NotContains(t, qStr, "proposer_mod_id", "Aucun champ proposer_mod_id ne doit être sérialisé dans la file")
	assert.NotContains(t, qStr, "validator_mod_id", "Aucun champ validator_mod_id ne doit être sérialisé dans la file")
	assert.NotContains(t, qStr, "claimed_by_mod_id", "Le champ claimed_by_mod_id doit être absent pour tout modérateur non-admin")
	assert.NotContains(t, qStr, "proposer_pseudonym", "Aucune information ni pseudonyme sur Mod A ne doit être sérialisé")

	// (a.3) Consultation directe du ticket par Mod B
	peerTicket, errGet := cache_service.GetModerationTicket(ctx, ticketID)
	require.NoError(t, errGet)
	assert.Equal(t, variables.ReportStatePendingPeerReview, peerTicket.State)

	view := ToPublicTicketView(peerTicket, false)
	assert.Equal(t, int64(0), view.ClaimedByModID, "ClaimedByModID doit être masqué (0) pour un modérateur non-admin")
	assert.Empty(t, view.ProposerPseudonym, "ProposerPseudonym doit être vide pour un non-admin")

	viewJSON, errVJSON := json.Marshal(view)
	require.NoError(t, errVJSON)
	vStr := string(viewJSON)
	assert.NotContains(t, vStr, "998877")
	assert.NotContains(t, vStr, "proposer_mod_id")
	assert.NotContains(t, vStr, "validator_mod_id")
	assert.NotContains(t, vStr, "claimed_by_mod_id")

	// (b) Vérification sur le corps des erreurs (Error bodies)
	// Mod A tente de s'auto-évaluer (interdit par la règle des 4 yeux)
	_, errSelfReview := ReviewBlameProposalService(ctx, modA, variables.UserGradeModerator, moderation_models.ReviewBlameInput{
		ReportID:  ticketID,
		Decision:  "validate",
		Rationale: "Tentative de contournement auto-validation",
	})
	require.Error(t, errSelfReview)
	assert.NotContains(t, errSelfReview.Error(), "998877", "Le message d'erreur ne doit jamais divulguer l'ID réel")

	if appErr, ok := errSelfReview.(*numan_error.AppError); ok {
		errBytes, errM := json.Marshal(appErr)
		require.NoError(t, errM)
		assert.NotContains(t, string(errBytes), "998877", "Le corps JSON de l'erreur ne doit jamais contenir l'ID réel")
	}

	// (c) Vérification sur les logs capturés
	capturedLogs := logBuf.String()
	assert.NotContains(t, capturedLogs, "998877", "Les logs structurés émis durant le cycle Peer-Review ne doivent jamais contenir l'ID de Mod A")

	// Validation légitime par Mod B
	reviewView, errReview := ReviewBlameProposalService(ctx, modB, variables.UserGradeModerator, moderation_models.ReviewBlameInput{
		ReportID:  ticketID,
		Decision:  "validate",
		Rationale: "Je confirme la décision après relecture du post.",
	})
	require.NoError(t, errReview)
	assert.Equal(t, variables.ReportStateClosed, reviewView.State)
}
