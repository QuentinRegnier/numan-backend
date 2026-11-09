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
// # SERVICE : BANNISSEMENT D'UN MEMBRE
// ############################################################################

// BanMember expulse et bannit définitivement un membre d'une conversation.
func BanMember(ctx context.Context, callerID int64, input member_models.BanMemberInput) (member_models.BanMemberOutput, error) {

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS ET HIERARCHIE ────────────────────────────
	callerMemberPayload, errSecurity := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errSecurity != nil {
		return member_models.BanMemberOutput{}, errSecurity
	}
	if callerMemberPayload.Role < variables.MemberRoleAdmin {
		return member_models.BanMemberOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous devez être administrateur ou propriétaire pour bannir un membre.", nil)
	}

	targetMemberPayload, errTargetSecurity := security_service.LeftMember(ctx, input.ConversationID, input.TargetUserID)
	if errTargetSecurity != nil {
		// S'il est déjà banni (-2) ou s'il a déjà quitté (-1), LeftMember renvoie une erreur.
		return member_models.BanMemberOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'utilisateur ciblé n'est pas un membre actif de ce groupe.", errTargetSecurity)
	}

	// Validation de la hiérarchie : On ne peut pas bannir un supérieur ou un égal
	if callerMemberPayload.Role <= targetMemberPayload.Role {
		return member_models.BanMemberOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous ne pouvez pas bannir un membre de rang égal ou supérieur au vôtre.", nil)
	}

	// ── ÉTAPE 2 : CRÉATION DU MESSAGE SYSTÈME ET GEL TEMPOREL ───────────────
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
			"sys_action": variables.SysActionMemberBanned,
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

	createMessageOutput, errCreateMsg := message_service.CreateMessage(ctx, callerID, input.ConversationID, systemMessageInput, true)

	// Détermination du plafond de gel (FrozenMessageID)
	frozenMessageID := createMessageOutput.MessageID
	if errCreateMsg != nil || createMessageOutput.MessageID == 0 {
		// Fallback de sécurité : Si le message système échoue, on gèle sur le dernier message connu
		conversationPayload, _ := object_cache_service.GetConversationFromObjectCache(ctx, input.ConversationID)
		frozenMessageID = conversationPayload.LastMessageID
	}

	// ── ÉTAPE 3 : APPLICATION DU BANNISSEMENT ───────────────────────────────
	targetMemberPayload.Role = variables.MemberRoleBanned
	targetMemberPayload.FrozenMessageID = frozenMessageID
	targetMemberPayload.UpdatedAt = domain.NowMillis()

	// ── ÉTAPE 4 : MISE À JOUR SYNCHRONE RAM L1 ──────────────────────────────
	_ = object_cache_service.SetMemberInObjectCache(ctx, targetMemberPayload)

	// Met à jour l'index Speed Cache (et retire implicitement le membre des listes de Fan-Out)
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
		numan_log.Error(ctx).Err(errQueue).Int64("user_id", targetMemberPayload.UserID).Msg("Échec du Write-Behind pour le bannissement d'un membre")
		return member_models.BanMemberOutput{}, numan_error.NewInternal()
	}

	// ── ÉTAPE 6 : DIFFUSION WEBSOCKET (ASYNCHRONE) ──────────────────────────
	go func() {
		errBroadcast := realtime_service.BroadcastToConversation(context.Background(), input.ConversationID, "member.banned", targetMemberPayload)
		if errBroadcast != nil {
			numan_log.Error(ctx).Err(errBroadcast).Msg("Échec de diffusion WebSocket pour le bannissement")
		}
	}()

	// ── ÉTAPE 7 : MARQUAGE D'ACTIVITÉ (DIRTY FLAG) ──────────────────────────
	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, callerID)

	return member_models.BanMemberOutput{
		InboxUpdateAt: latestActivityTimestampMs,
	}, nil
}
