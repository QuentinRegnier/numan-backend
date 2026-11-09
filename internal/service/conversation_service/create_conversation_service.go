package conversation_service

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : CRÉATION DE CONVERSATION (MP OU GROUPE)
// ############################################################################

// CreateConversation valide les règles d'intégrité, instancie la conversation
// et initialise ses participants en base et en cache.
func CreateConversation(ctx context.Context, callerID int64, input conversation_models.CreateConversationInput) (conversation_models.CreateConversationOutput, error) {

	// ── ÉTAPE 1 : VALIDATIONS MÉTIER PRÉLIMINAIRES ─────────────────────────
	if input.Type == variables.ConversationTypeCommunityPriv || input.Type == variables.ConversationTypeCommunityPub {
		return conversation_models.CreateConversationOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "La création de communautés requiert le point d'entrée dédié.", nil)
	}

	if input.Type == variables.ConversationTypeDirect && len(input.ParticipantIDs) != 1 {
		return conversation_models.CreateConversationOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Un message privé doit comporter exactement un participant cible.", nil)
	}

	if input.Type == variables.ConversationTypeGroup && pkg.CleanStr(input.Title) == "" {
		return conversation_models.CreateConversationOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Un groupe requiert obligatoirement un titre valide.", nil)
	}

	// ── ÉTAPE 2 : CONTRÔLE DE CONFIDENTIALITÉ DES MESSAGES PRIVÉS ──────────
	if input.Type == variables.ConversationTypeDirect {
		targetUserID := input.ParticipantIDs[0]
		if targetUserID == callerID {
			return conversation_models.CreateConversationOutput{}, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Création de conversation avec soi-même interdite.", nil)
		}

		targetUserLite, errLite := cache_service.GetUserLite(ctx, targetUserID)
		if errLite != nil || targetUserLite.ID == 0 {
			return conversation_models.CreateConversationOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Utilisateur cible introuvable.", errLite)
		}

		relationState := cache_service.RelationValue(ctx, callerID, targetUserID)
		if relationState == -1 {
			return conversation_models.CreateConversationOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Action impossible : utilisateur bloqué.", nil)
		}

		canInitiateDirectMessage := false
		switch targetUserLite.ConversationPermission {
		case 0:
			canInitiateDirectMessage = true
		case 1:
			canInitiateDirectMessage = relationState >= variables.RelationStateFollow
		case 2:
			canInitiateDirectMessage = relationState == variables.RelationStateFriend
		}

		if !canInitiateDirectMessage {
			return conversation_models.CreateConversationOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Cet utilisateur refuse la réception de messages privés.", nil)
		}
	}

	// ── ÉTAPE 3 : INITIALISATION DE L'ENTITÉ CONVERSATION ───────────────────
	currentTime := domain.NowTime()
	conversationID := pkg.GenerateID()

	cleanedTitle := ""
	if input.Type == variables.ConversationTypeGroup {
		cleanedTitle = pkg.CleanStr(input.Title)
	}

	conversationSettings := input.Settings
	if conversationSettings == (conversation_models.ConversationSettings{}) {
		conversationSettings = conversation_models.DefaultConversationSettings(input.Type)
	}

	conversationPayload := conversation_models.ConversationPayload{
		ID:           conversationID,
		Type:         input.Type,
		Title:        cleanedTitle,
		Description:  "",
		AvatarID:     0,
		State:        0,
		Settings:     conversationSettings,
		ExternalLink: models.ExternalLinks{},
		CreatedAt:    domain.TimeToMillis(currentTime),
		UpdatedAt:    domain.TimeToMillis(currentTime),
	}

	_ = object_cache_service.SetConversationInObjectCache(ctx, conversationPayload)

	errEnqueueConv := redis.EnqueueDB(ctx, conversationID, conversationID, redis.EntityConversation, redis.ActionCreate, conversationPayload, redis.TargetAll)
	if errEnqueueConv != nil {
		numan_log.Error(ctx).Err(errEnqueueConv).Int64("conv_id", conversationID).Msg("Échec d'enregistrement asynchrone de la conversation")
		return conversation_models.CreateConversationOutput{}, numan_error.NewInternal()
	}

	output := conversation_models.CreateConversationOutput{
		ConversationID: conversationID,
	}

	// ── ÉTAPE 4 : PEUPLEMENT ET EXPÉDITION DES PARTICIPANTS ─────────────────
	if input.Type == variables.ConversationTypeDirect {
		directMembers := []int64{callerID, input.ParticipantIDs[0]}
		for _, memberID := range directMembers {
			memberPayload := member_models.MemberPayload{
				ID:                pkg.GenerateID(),
				ConversationID:    conversationID,
				UserID:            memberID,
				Role:              variables.MemberRoleNormal,
				Settings:          member_models.DefaultMemberSettings(conversationPayload.Type),
				JoinedAt:          domain.TimeToMillis(currentTime),
				FrozenMessageID:   0,
				LastReadMessageID: 0,
				UnreadCount:       0,
				CreatedAt:         domain.TimeToMillis(currentTime),
				UpdatedAt:         domain.TimeToMillis(currentTime),
			}

			_ = object_cache_service.SetMemberInObjectCache(ctx, memberPayload)
			_ = cache_service.AddMemberToSpeedCache(ctx, lite_models.MemberLiteRequest{
				ConversationID:    memberPayload.ConversationID,
				UserID:            memberPayload.UserID,
				Role:              memberPayload.Role,
				Settings:          service.ToMemberSettingsLite(memberPayload.Settings),
				FrozenMessageID:   memberPayload.FrozenMessageID,
				LastReadMessageID: memberPayload.LastReadMessageID,
				UnreadCount:       memberPayload.UnreadCount,
				JoinedAt:          memberPayload.JoinedAt,
			})

			_ = redis.EnqueueDB(ctx, memberPayload.ID, conversationID, redis.EntityMembers, redis.ActionCreate, memberPayload, redis.TargetAll)
		}

		_ = realtime_service.DistributeToUsers(ctx, variables.NotificationConversationCreated, conversationPayload, directMembers)

	} else if input.Type == variables.ConversationTypeGroup {
		ownerPayload := member_models.MemberPayload{
			ID:                pkg.GenerateID(),
			ConversationID:    conversationID,
			UserID:            callerID,
			Role:              variables.MemberRoleOwner,
			Settings:          member_models.DefaultMemberSettings(conversationPayload.Type),
			JoinedAt:          domain.TimeToMillis(currentTime),
			FrozenMessageID:   0,
			LastReadMessageID: 0,
			UnreadCount:       0,
			CreatedAt:         domain.TimeToMillis(currentTime),
			UpdatedAt:         domain.TimeToMillis(currentTime),
		}

		_ = object_cache_service.SetMemberInObjectCache(ctx, ownerPayload)
		_ = cache_service.AddMemberToSpeedCache(ctx, lite_models.MemberLiteRequest{
			ConversationID:    ownerPayload.ConversationID,
			UserID:            ownerPayload.UserID,
			Role:              ownerPayload.Role,
			Settings:          service.ToMemberSettingsLite(ownerPayload.Settings),
			FrozenMessageID:   ownerPayload.FrozenMessageID,
			LastReadMessageID: ownerPayload.LastReadMessageID,
			UnreadCount:       ownerPayload.UnreadCount,
			JoinedAt:          ownerPayload.JoinedAt,
		})

		_ = redis.EnqueueDB(ctx, ownerPayload.ID, conversationID, redis.EntityMembers, redis.ActionCreate, ownerPayload, redis.TargetAll)

		if len(input.ParticipantIDs) > 0 {
			addMembersInput := conversation_models.AddMemberInput{
				ConversationID: conversationID,
				ParticipantIDs: input.ParticipantIDs,
			}
			addMembersOutput, err := AddMembersToConversation(ctx, callerID, addMembersInput)
			if err != nil {
				return output, err
			}

			output.AddedUserIDs = addMembersOutput.AddedUserIDs
			output.InvitedUserIDs = addMembersOutput.InvitedUserIDs
			output.RejectedUserIDs = addMembersOutput.RejectedUserIDs
			output.MessageIDs = addMembersOutput.MessageIDs
			output.ConversationIDs = addMembersOutput.ConversationIDs
		}

		_ = realtime_service.DistributeToUsers(ctx, variables.NotificationConversationCreated, conversationPayload, []int64{callerID})
	}

	// ── ÉTAPE 5 : TOUCH ACTIVITÉ FINALE ─────────────────────────────────────
	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, callerID)
	output.InboxUpdateAt = latestActivityTimestampMs

	return output, nil
}
