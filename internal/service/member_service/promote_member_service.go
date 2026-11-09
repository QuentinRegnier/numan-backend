package member_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/message_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : PROMOTION D'UN MEMBRE (ADMINISTRATION)
// ############################################################################

// PromoteMember promeut un membre standard au rang d'administrateur (Rôle = 1).
func PromoteMember(ctx context.Context, callerID int64, input member_models.PromoteMemberInput) (member_models.PromoteMemberOutput, error) {

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS DU DÉCIDEUR (PROPRIÉTAIRE) ───────────────

	callerMemberPayload, errSecurity := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errSecurity != nil {
		return member_models.PromoteMemberOutput{}, errSecurity
	}

	if callerMemberPayload.Role != variables.MemberRoleOwner {
		return member_models.PromoteMemberOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Seul le propriétaire actuel peut promouvoir un membre.", nil)
	}

	// ── ÉTAPE 2 : VÉRIFICATION DE LA CIBLE ET IDEMPOTENCE ───────────────────

	targetMemberPayload, errTargetSecurity := security_service.LeftMember(ctx, input.ConversationID, input.TargetUserID)
	if errTargetSecurity != nil {
		return member_models.PromoteMemberOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'utilisateur ciblé n'est pas un membre actif de ce groupe.", errTargetSecurity)
	}

	// Idempotence absolue : Si le membre est déjà Admin (1) ou Propriétaire (2), on ignore silencieusement.
	if targetMemberPayload.Role >= variables.MemberRoleAdmin {
		return member_models.PromoteMemberOutput{}, nil
	}

	// ── ÉTAPE 3 : NOTIFICATION SYSTÈME DANS LE FLUX DE DISCUSSION ───────────
	callerUserLite, errCaller := cache_service.GetUserLite(ctx, callerID)
	targetUserLite, errTarget := cache_service.GetUserLite(ctx, input.TargetUserID)

	callerUsername := "Unknown User"
	if errCaller == nil {
		callerUsername = callerUserLite.Username
	}
	targetUsername := "Unknown User"
	if errTarget == nil {
		targetUsername = targetUserLite.Username
	}

	systemMessageInput := message_models.CreateMessageInput{
		MessageType: variables.MessageTypeSystem,
		Content:     "",
		Attachments: map[string]any{
			"sys_action": variables.SysActionMemberPromoted,
			"actor": map[string]any{
				"id":       callerID,
				"username": callerUsername,
			},
			"target": map[string]any{
				"id":       input.TargetUserID,
				"username": targetUsername,
			},
		},
	}
	_, _ = message_service.CreateMessage(ctx, callerID, input.ConversationID, systemMessageInput, true)

	// ── ÉTAPE 4 : APPLICATION DE LA PROMOTION ───────────────────────────────

	targetMemberPayload.Role = variables.MemberRoleAdmin
	targetMemberPayload.UpdatedAt = domain.NowMillis()

	// ── ÉTAPE 5 : MISE À JOUR SYNCHRONE (OBJECT ET SPEED CACHE L1) ──────────

	_ = object_cache_service.SetMemberInObjectCache(ctx, targetMemberPayload)

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

	// ── ÉTAPE 6 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────

	errQueue := redis.EnqueueDB(ctx, targetMemberPayload.ID, input.ConversationID, redis.EntityMembers, redis.ActionUpdate, targetMemberPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("user_id", targetMemberPayload.UserID).Msg("Échec du Write-Behind pour la promotion d'un membre")
		return member_models.PromoteMemberOutput{}, numan_error.NewInternal()
	}

	// ── ÉTAPE 7 : DIFFUSION WEBSOCKET (ASYNCHRONE) ──────────────────────────

	go func() {
		errBroadcast := realtime_service.BroadcastToConversation(context.Background(), input.ConversationID, "member.promoted", targetMemberPayload)
		if errBroadcast != nil {
			numan_log.Error(ctx).Err(errBroadcast).Msg("Échec de la diffusion WebSocket pour member.promoted")
		}
	}()

	// ── ÉTAPE 8 : MARQUAGE D'ACTIVITÉ (DIRTY FLAG) ──────────────────────────

	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, callerID)

	return member_models.PromoteMemberOutput{
		InboxUpdateAt: latestActivityTimestampMs,
	}, nil
}
