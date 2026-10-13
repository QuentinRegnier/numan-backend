package conversation_service

import (
	"context"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/conversation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/member_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/media_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/message_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/notification_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/realtime_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/security_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # SERVICE : AJOUT DE MEMBRES À UNE CONVERSATION
// ############################################################################

// AddMembersToConversation orchestre l'ajout direct de membres ou l'envoi d'invitations
// en appliquant les matrices de confidentialité et les autorisations de groupe.
func AddMembersToConversation(ctx context.Context, callerID int64, input conversation_models.AddMemberInput) (conversation_models.AddMemberOutput, error) {
	numan_log.Info(ctx).
		Entity("conversation", input.ConversationID).
		Int("participants_count", len(input.ParticipantIDs)).
		Action(numan_log.ActionUpdate).
		Msg("Traitement d'une demande d'ajout de membres")

	// ── ÉTAPE 1 : CONTRÔLE D'ACCÈS DU DEMANDEUR (CALLER) ────────────────────
	callerMember, errSecurity := security_service.LeftMember(ctx, input.ConversationID, callerID)
	if errSecurity != nil || callerMember.Role < variables.MemberRoleNormal {
		return conversation_models.AddMemberOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Accès refusé : vous ne faites pas partie de cette conversation.", errSecurity)
	}

	// ── ÉTAPE 2 : VÉRIFICATION DE LA CONVERSATION ────────────────────────────
	conversationPayload, errConv := object_cache_service.GetConversationFromObjectCache(ctx, input.ConversationID)
	if errConv != nil || conversationPayload.State != variables.ConversationStateAll {
		return conversation_models.AddMemberOutput{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Conversation introuvable ou inactive.", errConv)
	}

	if conversationPayload.Type == variables.ConversationTypeDirect {
		return conversation_models.AddMemberOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Impossible d'ajouter des participants à un message privé individuel.", nil)
	}

	if !conversationPayload.Settings.AddMemberPermission && callerMember.Role == variables.MemberRoleNormal {
		return conversation_models.AddMemberOutput{}, numan_error.NewForbidden(numan_error.CodeForbidden, "Seuls les administrateurs peuvent ajouter des membres à ce groupe.", nil)
	}

	output := conversation_models.AddMemberOutput{
		AddedUserIDs:    make([]int64, 0),
		InvitedUserIDs:  make([]int64, 0),
		RejectedUserIDs: make([]int64, 0),
		MessageIDs:      make([]int64, 0),
		ConversationIDs: make([]int64, 0),
	}

	currentTime := time.Now().UTC()

	// ── ÉTAPE 3 : FILTRAGE ET TRAITEMENT DES CANDIDATS ──────────────────────
	if err := pkg.ListLimitVerif(input.ParticipantIDs, variables.MaxAddMemberCount); err != nil {
		return conversation_models.AddMemberOutput{}, err
	}

	for _, targetUserID := range input.ParticipantIDs {
		if targetUserID == callerID {
			continue // Exclusion de l'émetteur
		}

		// A. Empreinte de confidentialité via Speed Cache L1
		targetUserLite, errUserLite := cache_service.GetUserLite(ctx, targetUserID)
		if errUserLite != nil || targetUserLite.ID == 0 {
			output.RejectedUserIDs = append(output.RejectedUserIDs, targetUserID)
			continue
		}

		// B. Évaluation relationnelle (Target -> Caller)
		relationState := cache_service.RelationValue(ctx, callerID, targetUserID)
		if relationState == variables.RelationStateBlocked {
			// Blocage actif
			output.RejectedUserIDs = append(output.RejectedUserIDs, targetUserID)
			continue
		}

		// Contrôle de permission de contact
		canCommunicate := false
		switch targetUserLite.ConversationPermission {
		case 0:
			canCommunicate = true
		case 1:
			canCommunicate = relationState >= variables.RelationStateFollow // Abonné ou ami
		case 2:
			canCommunicate = relationState == variables.RelationStateFriend // Ami uniquement
		}

		if !canCommunicate {
			output.RejectedUserIDs = append(output.RejectedUserIDs, targetUserID)
			continue
		}

		// Contrôle de permission d'ajout direct
		canAddDirectly := false
		switch targetUserLite.AddGroupPermission {
		case 0:
			canAddDirectly = true
		case 1:
			canAddDirectly = relationState == variables.RelationStateFriend
		case 2:
			canAddDirectly = false
		}

		if canAddDirectly {
			// ── CAS A : INTÉGRATION IMMÉDIATE (MEMBRE DIRECT) ───────────────
			memberPayload := member_models.MemberPayload{
				ID:                pkg.GenerateID(),
				ConversationID:    conversationPayload.ID,
				UserID:            targetUserID,
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
				ConversationID:    conversationPayload.ID,
				UserID:            targetUserID,
				Role:              variables.MemberRoleNormal,
				Settings:          service.ToMemberSettingsLite(member_models.DefaultMemberSettings(conversationPayload.Type)),
				FrozenMessageID:   0,
				LastReadMessageID: 0,
				UnreadCount:       0,
				JoinedAt:          memberPayload.JoinedAt,
			})

			errEnqueue := redis.EnqueueDB(ctx, memberPayload.ID, conversationPayload.ID, redis.EntityMembers, redis.ActionCreate, memberPayload, redis.TargetAll)
			if errEnqueue != nil {
				numan_log.Error(ctx).
					Err(errEnqueue).
					Entity(string(redis.EntityMembers), memberPayload.ID).
					Action(numan_log.ActionCreate).
					Int64("target_user_id", targetUserID).
					Msg("Échec du Write-Behind lors de l'ajout d'un membre")
			}

			// Message système d'intégration
			callerLite, _ := cache_service.GetUserLite(ctx, callerID)

			systemMessageInput := message_models.CreateMessageInput{
				MessageType: variables.MessageTypeSystem,
				Content:     "",
				Attachments: map[string]any{
					"sys_action": variables.SysActionMemberJoinedThanksTo,
					"actor": map[string]any{
						"id":       callerLite.ID,
						"username": callerLite.Username,
					},
					"target": map[string]any{
						"id":       targetUserLite.ID,
						"username": targetUserLite.Username,
					},
				},
			}
			// Expédition du message de bienvenue système
			_, _ = message_service.CreateMessage(ctx, callerID, input.ConversationID, systemMessageInput, true)

			output.AddedUserIDs = append(output.AddedUserIDs, targetUserID)

			// Diffusion temps réel asynchrone
			go func(payload member_models.MemberPayload, convID int64, targetID int64, convType int, initiatorID int64) {
				backgroundContext := context.Background()

				isUserOnline := cache_service.IsUserOnline(backgroundContext, payload.UserID)
				if !targetUserLite.ShowOnlineStatus {
					isUserOnline = false
				}

				memberView := member_models.MemberView{
					MemberPayload: payload,
					IsOnline:      isUserOnline,
				}

				if liteData, errLite := cache_service.GetUserLite(backgroundContext, payload.UserID); errLite == nil {
					memberView.Username = liteData.Username

					if convType == variables.ConversationTypeCommunityPriv || convType == variables.ConversationTypeCommunityPub {
						memberView.AvatarCommunityID = liteData.ProfilePictureID
					} else if liteData.ProfilePictureID > 0 {
						if avatarView, errMedia := media_service.GenerateMediaViewCascade(backgroundContext, liteData.ProfilePictureID, payload.UserID, 0, initiatorID); errMedia == nil {
							memberView.Avatar = avatarView
						}
					}
				}

				_ = realtime_service.BroadcastToConversation(backgroundContext, convID, "member.added", memberView)
				_ = realtime_service.DistributeToUsers(backgroundContext, variables.NotificationConversationCreated, conversationPayload, []int64{targetID})

				// Signalement dans le registre de synchronisation
				participantsList, _ := redis.ConvParticipants.SMembers(backgroundContext, convID)
				participantIDs := pkg.ParseInt64List(participantsList)
				_ = cache_service.RecordConversationMutation(backgroundContext, convID, participantIDs)
			}(memberPayload, conversationPayload.ID, targetUserID, conversationPayload.Type, callerID)

		} else {
			// ── CAS B : ENVOI D'UNE INVITATION (MESSAGE TYPE 6) ─────────────
			directConvID, errDirect := getOrCreateDirectConversation(ctx, callerID, targetUserID)
			if errDirect == nil {
				inviteAttachments := map[string]any{
					"conversation_id": conversationPayload.ID,
				}

				inviteMessageInput := message_models.CreateMessageInput{
					MessageType: variables.MessageTypeInvite,
					Content:     "",
					Attachments: inviteAttachments,
				}

				messageOutput, errCreateMsg := message_service.CreateMessage(ctx, callerID, directConvID, inviteMessageInput, true)
				if errCreateMsg == nil && messageOutput.MessageID > 0 {
					output.MessageIDs = append(output.MessageIDs, messageOutput.MessageID)
					output.ConversationIDs = append(output.ConversationIDs, directConvID)
					output.InvitedUserIDs = append(output.InvitedUserIDs, targetUserID)

					go func(invitedID int64, sourceConvID int64) {
						_ = notification_service.DispatchNotification(context.Background(), invitedID, callerID, variables.EventGroupInvited, sourceConvID)
					}(targetUserID, conversationPayload.ID)
				}
			} else {
				output.RejectedUserIDs = append(output.RejectedUserIDs, targetUserID)
			}
		}
	}

	// ── ÉTAPE 4 : HORODATAGE DE L'ACTIVITÉ (DIRTY FLAG) ─────────────────────
	latestActivityTimestampMs := cache_service.TouchInboxActivity(ctx, callerID)
	output.InboxUpdateAt = domain.TimeToMillis(time.UnixMilli(latestActivityTimestampMs))

	return output, nil
}
