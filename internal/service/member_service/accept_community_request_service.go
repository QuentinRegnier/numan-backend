package member_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/message_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : ACCEPTATION D'UNE CANDIDATURE EN COMMUNAUTÉ
// ############################################################################

// AcceptCommunityRequest valide l'adhésion d'un membre en attente et l'intègre officiellement.
func AcceptCommunityRequest(ctx context.Context, callerID int64, input member_models.AcceptCommunityRequestInput) (member_models.AcceptCommunityRequestOutput, error) {

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS DU DÉCIDEUR (ADMIN OU OWNER) ─────────────
	callerMemberPayload, errSecurity := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errSecurity != nil {
		return member_models.AcceptCommunityRequestOutput{}, errSecurity
	}
	if callerMemberPayload.Role < variables.MemberRoleAdmin {
		return member_models.AcceptCommunityRequestOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Vous devez être administrateur pour accepter une candidature.", nil)
	}

	// ── ÉTAPE 2 : RÉCUPÉRATION DE LA CANDIDATURE (CASCADE L1 -> L2 -> L3) ───
	// Récupération manuelle car security_service.LeftMember bloque par défaut les rôles < 0
	var targetMemberPayload member_models.MemberPayload

	targetMemberPayload, errCache := object_cache_service.GetMemberFromObjectCache(ctx, input.ConversationID, input.TargetUserID)
	if errCache != nil || targetMemberPayload.ID == 0 {
		var errMongo error
		targetMemberPayload, errMongo = mongo.MongoGetMember(ctx, input.ConversationID, input.TargetUserID)

		if errMongo != nil || targetMemberPayload.ID == 0 {
			var errPg error
			targetMemberPayload, errPg = postgres.FuncGetMember(ctx, input.ConversationID, input.TargetUserID)
			if errPg != nil {
				numan_log.Error(ctx).Err(errPg).Int64("user_id", input.TargetUserID).Msg("Échec L3 lors de la récupération du membre en attente")
				return member_models.AcceptCommunityRequestOutput{}, numan_error.NewInternal()
			}
			if targetMemberPayload.ID == 0 {
				return member_models.AcceptCommunityRequestOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Candidature introuvable.", nil)
			}
		}
	}

	// Règle métier : Vérifier que l'utilisateur est bien en attente d'approbation
	if targetMemberPayload.Role != variables.MemberRolePending {
		return member_models.AcceptCommunityRequestOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Cet utilisateur n'est pas en attente d'approbation.", nil)
	}

	// ── ÉTAPE 3 : APPLICATION DE L'INTÉGRATION ──────────────────────────────
	currentTimeMs := domain.NowMillis()

	targetMemberPayload.Role = variables.MemberRoleNormal
	targetMemberPayload.Settings = member_models.DefaultMemberSettings(variables.ConversationTypeCommunityPub)
	targetMemberPayload.JoinedAt = currentTimeMs
	targetMemberPayload.UpdatedAt = currentTimeMs

	// ── ÉTAPE 4 : MISE À JOUR SYNCHRONE DES CACHES RAM L1 ───────────────────
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

	// ── ÉTAPE 5 : PERSISTANCE ASYNCHRONE (WRITE-BEHIND) ─────────────────────
	errQueue := redis.EnqueueDB(ctx, targetMemberPayload.ID, input.ConversationID, redis.EntityMembers, redis.ActionUpdate, targetMemberPayload, redis.TargetAll)
	if errQueue != nil {
		numan_log.Error(ctx).Err(errQueue).Int64("member_id", targetMemberPayload.ID).Msg("Échec du Write-Behind pour l'acceptation de candidature")
		return member_models.AcceptCommunityRequestOutput{}, numan_error.NewInternal()
	}

	// ── ÉTAPE 6 : MESSAGE SYSTÈME ET DIFFUSION WEBSOCKET ────────────────────
	go func() {
		backgroundContext := context.Background()

		if targetUserLite, errLite := cache_service.GetUserLite(backgroundContext, targetMemberPayload.UserID); errLite == nil {

			// A. Publication du message système d'intégration
			callerUserLite, errCaller := cache_service.GetUserLite(backgroundContext, callerID)
			callerUsername := "Unknown User"
			if errCaller == nil {
				callerUsername = callerUserLite.Username
			}

			systemMessageInput := message_models.CreateMessageInput{
				MessageType: variables.MessageTypeSystem,
				Content:     "",
				Attachments: map[string]any{
					"sys_action": variables.SysActionMemberJoined,
					"actor": map[string]any{
						"id":       callerID,
						"username": callerUsername,
					},
					"target": map[string]any{
						"id":       targetMemberPayload.UserID,
						"username": targetUserLite.Username,
					},
				},
			}
			_, _ = message_service.CreateMessage(backgroundContext, callerID, input.ConversationID, systemMessageInput, true)

			// B. Préparation du DTO pour le WebSocket
			conversationPayload, _ := object_cache_service.GetConversationFromObjectCache(backgroundContext, input.ConversationID)

			memberViewDto := member_models.MemberView{
				MemberPayload: targetMemberPayload,
				Username:      targetUserLite.Username,
				IsOnline:      cache_service.IsUserOnline(backgroundContext, targetMemberPayload.UserID),
			}

			// Gestion des avatars selon le type de conversation
			if conversationPayload.Type == variables.ConversationTypeCommunityPriv || conversationPayload.Type == variables.ConversationTypeCommunityPub {
				memberViewDto.AvatarCommunityID = targetUserLite.ProfilePictureID // Mode Twitch
			} else if targetUserLite.ProfilePictureID > 0 {
				if mediaView, errMedia := media_service.GenerateMediaViewCascade(backgroundContext, targetUserLite.ProfilePictureID, targetMemberPayload.UserID, 0, callerID); errMedia == nil {
					memberViewDto.Avatar = mediaView
				}
			}

			// C. Diffusion WebSocket temps réel
			_ = realtime_service.BroadcastToConversation(backgroundContext, input.ConversationID, "member.joined", memberViewDto)
		}
	}()

	// ── ÉTAPE 7 : MARQUAGE D'ACTIVITÉ (DIRTY FLAG) ──────────────────────────
	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, callerID)

	return member_models.AcceptCommunityRequestOutput{
		InboxUpdateAt: latestActivityTimestampMs,
	}, nil
}
