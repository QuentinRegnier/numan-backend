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
// # SERVICE : DESTITUTION D'UN ADMINISTRATEUR (DEMOTE)
// ############################################################################

// DemoteMember rétrograde un administrateur au rang de membre standard (Rôle = 0).
func DemoteMember(ctx context.Context, callerID int64, input member_models.DemoteMemberInput) (member_models.DemoteMemberOutput, error) {

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS ET HIERARCHIE ────────────────────────────
	callerMemberPayload, errSecurity := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errSecurity != nil {
		return member_models.DemoteMemberOutput{}, errSecurity
	}
	if callerMemberPayload.Role != variables.MemberRoleOwner {
		return member_models.DemoteMemberOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Seul le propriétaire peut destituer un administrateur.", nil)
	}

	targetMemberPayload, errTargetSecurity := security_service.LeftMember(ctx, input.ConversationID, input.TargetUserID)
	if errTargetSecurity != nil {
		return member_models.DemoteMemberOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'utilisateur ciblé n'est pas un membre actif de ce groupe.", errTargetSecurity)
	}

	// ── ÉTAPE 2 : RÈGLES MÉTIER ET IDEMPOTENCE ──────────────────────────────
	if targetMemberPayload.Role == variables.MemberRoleNormal {
		return member_models.DemoteMemberOutput{}, nil // Déjà membre normal, réussite silencieuse
	}
	if targetMemberPayload.Role == variables.MemberRoleOwner {
		return member_models.DemoteMemberOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Impossible de destituer le propriétaire. Transférez d'abord la propriété.", nil)
	}

	// ── ÉTAPE 3 : MESSAGE SYSTÈME DE NOTIFICATION ───────────────────────────
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
			"sys_action": variables.SysActionMemberDemoted,
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

	// ── ÉTAPE 4 : APPLICATION DE LA DESTITUTION ─────────────────────────────
	targetMemberPayload.Role = variables.MemberRoleNormal
	targetMemberPayload.UpdatedAt = domain.NowMillis()

	// ── ÉTAPE 5 : MISE À JOUR SYNCHRONE RAM L1 ──────────────────────────────
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
		numan_log.Error(ctx).Err(errQueue).Int64("user_id", targetMemberPayload.UserID).Msg("Échec du Write-Behind pour la destitution d'un administrateur")
		return member_models.DemoteMemberOutput{}, numan_error.NewInternal()
	}

	// ── ÉTAPE 7 : DIFFUSION WEBSOCKET (ASYNCHRONE) ──────────────────────────
	go func() {
		errBroadcast := realtime_service.BroadcastToConversation(context.Background(), input.ConversationID, "member.demoted", targetMemberPayload)
		if errBroadcast != nil {
			numan_log.Error(ctx).Err(errBroadcast).Msg("Échec de diffusion WebSocket pour la destitution")
		}
	}()

	// ── ÉTAPE 8 : MARQUAGE D'ACTIVITÉ (DIRTY FLAG) ──────────────────────────
	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, callerID)

	return member_models.DemoteMemberOutput{
		InboxUpdateAt: latestActivityTimestampMs,
	}, nil
}
