package member_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : REFUS D'UNE CANDIDATURE EN COMMUNAUTÉ
// ############################################################################

// RefuseCommunityRequest rejette une candidature en attente et passe le membre au rôle -4 (Rejeté).
func RefuseCommunityRequest(ctx context.Context, callerID int64, input member_models.RefuseCommunityRequestInput) (member_models.RefuseCommunityRequestOutput, error) {

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS DU DÉCIDEUR ──────────────────────────────

	callerMemberPayload, errSecurity := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errSecurity != nil {
		return member_models.RefuseCommunityRequestOutput{}, errSecurity
	}

	if callerMemberPayload.Role < variables.MemberRoleAdmin {
		return member_models.RefuseCommunityRequestOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous devez être administrateur pour rejeter une candidature.", nil)
	}

	// ── ÉTAPE 2 : RÉCUPÉRATION DE LA CANDIDATURE (CASCADE L1 -> L2 -> L3) ───

	var targetMemberPayload member_models.MemberPayload

	targetMemberPayload, errCache := object_cache_service.GetMemberFromObjectCache(ctx, input.ConversationID, input.TargetUserID)
	if errCache != nil || targetMemberPayload.ID == 0 {

		var errMongo error
		targetMemberPayload, errMongo = mongo.MongoGetMember(ctx, input.ConversationID, input.TargetUserID)

		if errMongo != nil || targetMemberPayload.ID == 0 {
			var errPg error
			targetMemberPayload, errPg = postgres.FuncGetMember(ctx, input.ConversationID, input.TargetUserID)
			if errPg != nil {
				numan_log.Error(ctx).Err(errPg).Int64("user_id", input.TargetUserID).Msg("Échec L3 de la récupération du candidat refusé")
				return member_models.RefuseCommunityRequestOutput{}, numan_error.NewInternal()
			}
			if targetMemberPayload.ID == 0 {
				return member_models.RefuseCommunityRequestOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Candidature introuvable.", nil)
			}

			// AUTO-GUÉRISON L3 -> L2
			go func(m member_models.MemberPayload) {
				bgCtx := context.Background()
				_ = redis.EnqueueDB(bgCtx, m.ID, m.ConversationID, redis.EntityMembers, redis.ActionUpdate, m, redis.TargetMongo)
			}(targetMemberPayload)
		}

		// AUTO-GUÉRISON L3/L2 -> L1
		_ = object_cache_service.SetMemberInObjectCache(ctx, targetMemberPayload)
	}

	if targetMemberPayload.Role != variables.MemberRolePending {
		return member_models.RefuseCommunityRequestOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Cet utilisateur n'est pas en attente d'approbation.", nil)
	}

	// ── ÉTAPE 3 : APPLICATION DU REJET ──────────────────────────────────────

	targetMemberPayload.Role = variables.MemberRolePendingApproval // Devient -4 (Rejeté)
	targetMemberPayload.UpdatedAt = domain.NowMillis()

	// ── ÉTAPE 4 : MISE À JOUR SYNCHRONE DES CACHES RAM L1 ───────────────────

	_ = object_cache_service.SetMemberInObjectCache(ctx, targetMemberPayload)

	// Retrait de la communauté de l'inbox de l'utilisateur pour ne pas polluer sa liste
	_ = cache_service.RemoveMemberFromSpeedCache(ctx, input.ConversationID, input.TargetUserID)

	// Mais on maintient son empreinte Lite dans le SpeedCache avec son nouveau rôle pour les vérifications rapides
	liteMemberRequest := lite_models.MemberLiteRequest{
		ConversationID:    targetMemberPayload.ConversationID,
		UserID:            targetMemberPayload.UserID,
		Role:              targetMemberPayload.Role,
		Settings:          service.ToMemberSettingsLite(targetMemberPayload.Settings),
		UnreadCount:       targetMemberPayload.UnreadCount,
		FrozenMessageID:   targetMemberPayload.FrozenMessageID,
		LastReadMessageID: targetMemberPayload.LastReadMessageID,
		JoinedAt:          targetMemberPayload.JoinedAt,
	}
	_ = cache_service.UpdateMemberSpeedCache(ctx, liteMemberRequest)

	// ── ÉTAPE 5 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────

	errQueue := redis.EnqueueDB(ctx, targetMemberPayload.ID, input.ConversationID, redis.EntityMembers, redis.ActionUpdate, targetMemberPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("user_id", targetMemberPayload.UserID).Msg("Échec du Write-Behind pour le rejet d'une candidature")
		return member_models.RefuseCommunityRequestOutput{}, numan_error.NewInternal()
	}

	// ── ÉTAPE 6 : NOTIFICATION TEMPS RÉEL SILENCIEUSE ───────────────────────

	go func() {
		bgCtx := context.Background()
		// Notification invisible pour le frontend, permet simplement la mise à jour de l'UI (ex: bouton "Demande envoyée" -> "Refusée")
		socketPayload := map[string]interface{}{
			"conversation_id": input.ConversationID,
			"role":            variables.MemberRolePendingApproval,
		}
		_ = realtime_service.DistributeToUsers(bgCtx, variables.NotificationCommunityRequestRefused, socketPayload, []int64{input.TargetUserID})
	}()

	// ── ÉTAPE 7 : MARQUAGE D'ACTIVITÉ (DIRTY FLAG) ──────────────────────────

	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, callerID)

	return member_models.RefuseCommunityRequestOutput{
		InboxUpdateAt: latestActivityTimestampMs,
	}, nil
}
